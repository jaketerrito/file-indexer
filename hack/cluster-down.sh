#!/usr/bin/env bash
# Destroy the SHARED kind cluster and every checkout's stack with it.
# The registry container may stay.
#
# The kindccm-gw-* cleanup is load-bearing: cloud-provider-kind's
# per-gateway data planes are plain docker containers on the 'kind' network;
# they survive cluster deletion and poison a recreated gateway with a stale
# xDS address (symptom: envoy returns 404s while HTTPRoutes show Accepted).
set -euo pipefail
cd "$(dirname "$0")/.."

docker rm -f cloud-provider-kind kind-gateway-proxy $(docker ps -aq --filter name=^kindccm-gw-) 2>/dev/null || true
ctlptl delete -f hack/cluster/cluster.yaml
