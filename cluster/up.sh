#!/usr/bin/env bash
# Create the shared dev cluster (one-time per machine, safe to re-run). The
# shared cluster serves every checkout of this repo: ctlptl registry+kind
# cluster, cloud-provider-kind (Gateway API CRDs + LB), the shared Gateway,
# and an IPv4-only loopback forwarder publishing the gateway on 127.0.0.1:80.
# Prereqs: docker, kubectl, ctlptl. Usually run via `just cluster-up`.
set -euo pipefail
cd "$(dirname "$0")"
ctlptl apply -f ctlptl.yaml
# kind marks the (single) control-plane node as excluded from external load
# balancers; cloud-provider-kind routes via cluster nodes, so drop the
# exclusion (trailing '-' removes the label).
kubectl --context kind-kind label node kind-control-plane node.kubernetes.io/exclude-from-external-load-balancers- || true
if ! docker start cloud-provider-kind 2>/dev/null; then
    docker run -d --name cloud-provider-kind --restart unless-stopped --network kind \
        -v /var/run/docker.sock:/var/run/docker.sock \
        registry.k8s.io/cloud-provider-kind/cloud-controller-manager:v0.11.1 >/dev/null
fi
# cloud-provider-kind installs the Gateway API CRDs during startup, not at
# container start: on a fresh cluster, poll until they exist or the apply
# below fails with "no matches for kind Gateway".
for _ in $(seq 1 60); do
    kubectl --context kind-kind get crd gateways.gateway.networking.k8s.io >/dev/null 2>&1 && break
    sleep 2
done
kubectl --context kind-kind apply -f gateway.yaml
kubectl --context kind-kind -n gateway-system wait --for=condition=Programmed gateway/shared-gateway --timeout=120s
gw_ip="$(kubectl --context kind-kind -n gateway-system get gateway shared-gateway -o jsonpath='{.status.addresses[0].value}')"
# IPv4-only forwarder: *.localhost resolves to ::1 first on systemd-resolved
# hosts, and the gateway's envoy is IPv4-only — a 127.0.0.1-only publish makes
# ::1 refuse the connection so every client falls back to IPv4. Recreated when
# the gateway address drifts (cluster recreate) or the publish spec doesn't
# match (the labels record the desired state; a stale container fails the filter).
if [[ -z "$(docker ps -aq --filter name=^kind-gateway-proxy$ --filter label=gateway-target="$gw_ip" --filter label=gateway-publish="127.0.0.1:80")" ]]; then
    docker rm -f kind-gateway-proxy >/dev/null 2>&1 || true
    docker run -d --name kind-gateway-proxy --restart unless-stopped --network kind \
        --label "gateway-target=$gw_ip" --label "gateway-publish=127.0.0.1:80" \
        -p 127.0.0.1:80:80 alpine/socat:1.8.1.3 \
        TCP-LISTEN:80,fork,reuseaddr "TCP:$gw_ip:8080" >/dev/null
else
    docker start kind-gateway-proxy >/dev/null
fi
