#!/usr/bin/env bash
# Create the shared dev cluster — one-time per machine, safe to re-run
# (every step is skipped when already satisfied):
#   ctlptl registry + kind cluster, cloud-provider-kind (Gateway API CRDs +
#   LB), the shared Gateway, and a socat loopback proxy publishing the
#   gateway on 127.0.0.1:18080 (fixed; IPv4-only on purpose — see below).
# Prereqs on PATH: docker, kubectl, ctlptl, kind. Teardown: hack/cluster-down.sh.
set -euo pipefail
cd "$(dirname "$0")/.."

ctlptl apply -f hack/cluster/registry.yaml
ctlptl apply -f hack/cluster/cluster.yaml
# cloud-provider-kind must reach the (single) control-plane node.
kubectl --context kind-kind label node kind-control-plane node.kubernetes.io/exclude-from-external-load-balancers- || true

if ! docker start cloud-provider-kind >/dev/null 2>&1; then
    docker run -d --name cloud-provider-kind --restart unless-stopped --network kind \
        -v /var/run/docker.sock:/var/run/docker.sock \
        registry.k8s.io/cloud-provider-kind/cloud-controller-manager:v0.11.1 >/dev/null
fi

# The provider installs the Gateway API CRDs and its GatewayClass.
kubectl --context kind-kind apply -f hack/cluster/gateway.yaml
kubectl --context kind-kind -n gateway-system wait --for=condition=Programmed gateway/shared-gateway --timeout=120s
gw_ip="$(kubectl --context kind-kind -n gateway-system get gateway shared-gateway -o jsonpath='{.status.addresses[0].value}')"

# Publish the gateway on IPv4 loopback ONLY, on a FIXED port (18080), via a
# tiny forwarder. The provider's own LB port-mapping also publishes [::],
# but envoy binds IPv4-only inside its container, so docker-proxy's IPv6
# path accept-then-resets — and *.localhost resolves to ::1 first on
# systemd-resolved hosts. An IPv4-only publish makes ::1 refuse the
# connection, so every client falls back to 127.0.0.1. Recreated when the
# gateway address drifts (cluster recreate).
if [[ "$(docker inspect -f '{{index .Config.Labels "gateway-target"}}' kind-gateway-proxy 2>/dev/null || true)" != "$gw_ip" ]]; then
    docker rm -f kind-gateway-proxy >/dev/null 2>&1 || true
    docker run -d --name kind-gateway-proxy --restart unless-stopped --network kind \
        --label "gateway-target=$gw_ip" -p 127.0.0.1:18080:80 alpine/socat:1.8.1.3 \
        TCP-LISTEN:80,fork,reuseaddr "TCP:$gw_ip:80" >/dev/null
else
    docker start kind-gateway-proxy >/dev/null
fi
echo "shared cluster 'kind-kind' ready; gateway published on 127.0.0.1:18080"
