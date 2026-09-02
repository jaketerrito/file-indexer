#!/usr/bin/env bash
# Destroy the SHARED cluster and every checkout's stack with it. The
# kindccm-gw-* cleanup is load-bearing: cloud-provider-kind's gateway data
# planes are plain docker containers on the 'kind' network; they survive
# cluster deletion and poison a recreated gateway with a stale xDS address
# (symptom: envoy returns 404s while HTTPRoutes show Accepted).
# Usually run via `just cluster-down`.
set -euo pipefail
cd "$(dirname "$0")"
docker rm -f cloud-provider-kind kind-gateway-proxy $(docker ps -aq --filter name=^kindccm-gw-) 2>/dev/null || true
ctlptl delete -f ctlptl.yaml
