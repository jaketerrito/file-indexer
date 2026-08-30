# Show available commands
help:
    @just --list

# Regenerate all generated code (sqlc, protobuf, web TS clients). The web
# codegen requires npm: protoc-gen-es comes from web/node_modules. Run this
# after touching migrations, sqlc queries, .proto files, or interfaces listed
# in .mockery.yaml, and commit the regenerated output.
generate:
    go generate ./...
    npm --prefix web ci
    go tool buf generate --template proto/buf.gen.web.yaml proto

# Run all linters (Go + Kubernetes manifests + protobuf + web TS)
lint: lint-go lint-k8s lint-proto lint-web

# Run golangci-lint checks on all Go code
lint-go:
    go tool golangci-lint run ./...

# Validate Kubernetes manifests with kubeconform (builds kustomize output first)
lint-k8s:
    go tool kustomize build deploy | go tool kubeconform -strict -summary

# Lint protobuf files with buf
lint-proto:
    go tool buf lint proto

# Lint + format-check web TypeScript with Biome
lint-web:
    npm --prefix web ci
    npm --prefix web run lint

# Auto-format all code (Go + YAML + protobuf + web TS)
fmt: fmt-go fmt-yaml fmt-proto fmt-web

# Auto-format Go code with golangci-lint
fmt-go:
    go tool golangci-lint fmt

# Auto-format YAML files with yamlfmt
fmt-yaml:
    go tool yamlfmt .

# Auto-format protobuf files with buf
fmt-proto:
    go tool buf format -w proto

# Auto-format (and apply safe lint fixes to) web TypeScript with Biome
fmt-web:
    npm --prefix web ci
    npm --prefix web run fmt

# Ensure the shared dev cluster and image registry exist. Every checkout
# shares ONE kind cluster ("kind-kind"); isolation is per-namespace — the
# main checkout deploys to "default" (identical to CI), worktrees to
# "wt-<slug>" (hack/dev-env.sh). Creation is strictly additive: an existing
# cluster or registry is never re-applied, so bringing up one checkout can
# never disturb another checkout's running stack. Also ensures the shared
# cloud-provider-kind container (LoadBalancer + Gateway API controller) and
# the shared Gateway (cluster/gateway.yaml) exist; every checkout's services
# are then reached via `<namespace>.<service>.localhost:$GATEWAY_PORT`.
# Fails early if this checkout's Tilt UI port is taken; override with
# WORKTREE_INDEX=<n> if two checkouts collide.
cluster-up:
    #!/usr/bin/env bash
    set -euo pipefail
    hack/dev-env.sh --check > /dev/null
    eval "$(hack/dev-env.sh)"
    if ! ctlptl get registry ctlptl-registry > /dev/null 2>&1; then
        ctlptl apply -f ctlptl.yaml
    fi
    if ! ctlptl get cluster "$CLUSTER_NAME" > /dev/null 2>&1; then
        cluster_yaml="$(mktemp)"
        trap 'rm -f "$cluster_yaml"' EXIT
        printf 'apiVersion: ctlptl.dev/v1alpha1\nkind: Cluster\nproduct: kind\nname: %s\nregistry: ctlptl-registry\n' "$CLUSTER_NAME" > "$cluster_yaml"
        ctlptl apply -f "$cluster_yaml"
    fi
    # cloud-provider-kind must reach the (single) control-plane node.
    kubectl --context "$K8S_CONTEXT" label node kind-control-plane \
        node.kubernetes.io/exclude-from-external-load-balancers- > /dev/null 2>&1 || true
    if ! docker ps --format '{{"{{"}}.Names{{"}}"}}' | grep -qx cloud-provider-kind; then
        if docker ps -a --format '{{"{{"}}.Names{{"}}"}}' | grep -qx cloud-provider-kind; then
            docker start cloud-provider-kind > /dev/null
        else
            docker run -d --name cloud-provider-kind --restart unless-stopped \
                --network kind -v /var/run/docker.sock:/var/run/docker.sock \
                registry.k8s.io/cloud-provider-kind/cloud-controller-manager:v0.11.1 > /dev/null
        fi
    fi
    # The provider installs the Gateway API CRDs and its GatewayClass.
    for _ in $(seq 1 60); do
        kubectl --context "$K8S_CONTEXT" get gatewayclass cloud-provider-kind > /dev/null 2>&1 && break
        sleep 1
    done
    if ! kubectl --context "$K8S_CONTEXT" get gatewayclass cloud-provider-kind > /dev/null 2>&1; then
        # Do NOT kubectl-apply the upstream standard-install CRDs as a
        # "fallback": its safe-upgrades ValidatingAdmissionPolicy forbids the
        # provider's embedded CRDs, breaking it permanently until the policy
        # is removed. The provider is broken; show why and stop.
        echo "cloud-provider-kind GatewayClass never appeared; provider logs:" >&2
        docker logs cloud-provider-kind 2>&1 | tail -20 >&2
        exit 1
    fi
    kubectl --context "$K8S_CONTEXT" apply -f cluster/gateway.yaml
    kubectl --context "$K8S_CONTEXT" -n gateway-system wait \
        --for=condition=Programmed gateway/shared-gateway --timeout=120s
    # Publish the gateway on IPv4 loopback ONLY via a tiny forwarder. The
    # provider's own -enable-lb-port-mapping also publishes [::], but envoy
    # binds IPv4-only inside its container, so docker-proxy's IPv6 path
    # accept-then-resets — and *.localhost resolves to ::1 first on
    # systemd-resolved hosts, killing curl and browsers with no fallback.
    # An IPv4-only publish makes ::1 refuse the connection instead, which
    # every client falls back from to 127.0.0.1.
    gw_ip="$(kubectl --context "$K8S_CONTEXT" -n gateway-system get gateway shared-gateway \
        -o jsonpath='{.status.addresses[0].value}')"
    [[ -n "$gw_ip" ]] || { echo "gateway shared-gateway has no address" >&2; exit 1; }
    cur="$(docker inspect kind-gateway-proxy \
        --format '{{"{{"}}index .Config.Labels "gateway-target"{{"}}"}}' 2>/dev/null || true)"
    if [[ "$cur" != "$gw_ip" ]] || ! docker ps -q --filter name=^kind-gateway-proxy$ | grep -q .; then
        docker rm -f kind-gateway-proxy > /dev/null 2>&1 || true
        docker run -d --name kind-gateway-proxy --restart unless-stopped \
            --network kind --label "gateway-target=$gw_ip" -p 127.0.0.1::80 \
            alpine/socat:1.8.1.3 \
            TCP-LISTEN:80,fork,reuseaddr "TCP:$gw_ip:80" > /dev/null
    fi
    echo "gateway: http://<namespace>.<web|s3|s3-console>.localhost:<GATEWAY_PORT> (see hack/dev-env.sh)"
    echo "shared cluster '${CLUSTER_NAME}' ready (context ${K8S_CONTEXT})"

# Destroy the SHARED kind cluster and its registry. This affects EVERY
# checkout, not just this one — every worktree's stack goes with it. For
# per-checkout teardown use `just down`.
cluster-down:
    #!/usr/bin/env bash
    set -euo pipefail
    docker rm -f cloud-provider-kind kind-gateway-proxy > /dev/null 2>&1 || true
    eval "$(hack/dev-env.sh)"
    cluster_yaml="$(mktemp)"
    trap 'rm -f "$cluster_yaml"' EXIT
    printf 'apiVersion: ctlptl.dev/v1alpha1\nkind: Cluster\nproduct: kind\nname: %s\nregistry: ctlptl-registry\n' "$CLUSTER_NAME" > "$cluster_yaml"
    ctlptl delete -f "$cluster_yaml" || true

# Start Tilt dev environment (background) in this checkout's namespace,
# creating it first — Tilt does not create namespaces. Worktrees get their
# own Tilt UI port (10350+N, see hack/dev-env.sh).
tilt-up:
    #!/usr/bin/env bash
    set -euo pipefail
    hack/dev-env.sh --check > /dev/null
    eval "$(hack/dev-env.sh)"
    # Tilt does not create namespaces; "default" exists in every cluster.
    if [[ "$K8S_NAMESPACE" != "default" ]]; then
        kubectl --context "$K8S_CONTEXT" create namespace "$K8S_NAMESPACE" \
            --dry-run=client -o yaml | kubectl --context "$K8S_CONTEXT" apply -f - > /dev/null
    fi
    nohup tilt up --context "$K8S_CONTEXT" --namespace "$K8S_NAMESPACE" --port "$TILT_PORT" > /dev/null 2>&1 &
    echo $! > .tilt.pid
    echo "web UI:  http://$WEB_HOST:$GATEWAY_PORT"
    echo "tilt UI: http://localhost:$TILT_PORT"
    xdg-open "http://$WEB_HOST:$GATEWAY_PORT" 2>/dev/null || true

# Tear down Tilt and delete this checkout's namespace, taking every
# resource in it along. Other checkouts' Tilt instances and namespaces are
# left alone; the shared cluster and registry survive. The main checkout
# uses "default", which k8s forbids deleting, so there `tilt down` alone
# does the cleanup.
tilt-down:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    if [[ -f .tilt.pid ]]; then
        pid="$(cat .tilt.pid)"
        if kill -0 "$pid" 2>/dev/null && [[ "$(cat "/proc/$pid/comm" 2>/dev/null || true)" == "tilt" ]]; then
            kill "$pid" || true
        fi
        rm -f .tilt.pid
    fi
    pkill -f "tilt up.*--port $TILT_PORT" 2>/dev/null || true
    tilt down --context "$K8S_CONTEXT" --namespace "$K8S_NAMESPACE" || true
    if [[ "$K8S_NAMESPACE" == "default" ]]; then
        echo "leaving the 'default' namespace in place; tilt down already removed this checkout's resources"
    else
        kubectl --context "$K8S_CONTEXT" delete namespace "$K8S_NAMESPACE" --ignore-not-found
    fi

# Ensure cluster + namespace, then start Tilt
up: cluster-up tilt-up

# Stop Tilt and delete this checkout's namespace (shared cluster survives)
down: tilt-down

# Deploy everything with auto_init=True (all services, postgres, MinIO,
# secrets, the lint local resource) and run the full test suite + coverage
# gate via the test-integration Tilt resource. Verifies real rollouts of every
# service, not just manifest validity. This is what CI runs; reproduce locally
# with `just cluster-up && just ci`.
ci:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    # Tilt does not create namespaces; "default" exists in every cluster.
    if [[ "$K8S_NAMESPACE" != "default" ]]; then
        kubectl --context "$K8S_CONTEXT" create namespace "$K8S_NAMESPACE" \
            --dry-run=client -o yaml | kubectl --context "$K8S_CONTEXT" apply -f - > /dev/null
    fi
    tilt ci --context "$K8S_CONTEXT" --namespace "$K8S_NAMESPACE" --port "$TILT_PORT"

# Run unit tests with race detector and write a coverage profile. Integration
# tests are excluded (build-tag gated); coverage thresholds are only checked by
# test-integration, since unit tests alone cannot reach them.
test:
    go test -race ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...

# Run Go tests with race detector and verbose output
test-verbose:
    go test -race -v ./...

# Run the full test suite (unit + integration), then check coverage thresholds
# from .testcoverage.yml. Services come from explicit DB_HOST/S3_ENDPOINT env
# (CI, against runner-local containers) or, when unset, from ephemeral kubectl
# port-forwards into this checkout's namespace (requires `just up`).
# Integration tests fail hard if the services are unreachable.
test-integration:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    pf_pids=()
    trap 'kill "${pf_pids[@]}" 2>/dev/null || true' EXIT
    if [[ -z "${DB_HOST:-}" || -z "${S3_ENDPOINT:-}" ]]; then
        for tgt in postgres:5432 local-s3:9000; do
            svc="${tgt%%:*}"; port="${tgt##*:}"
            log="$(mktemp)"
            kubectl --context "$K8S_CONTEXT" -n "$K8S_NAMESPACE" port-forward "svc/$svc" ":$port" > "$log" 2>&1 &
            pf_pids+=($!)
            for _ in $(seq 1 50); do grep -q "Forwarding from" "$log" 2>/dev/null && break; sleep 0.2; done
            fwd="$(grep -m1 -oE '127\.0\.0\.1:[0-9]+' "$log" | cut -d: -f2 || true)"
            [[ -n "$fwd" ]] || { echo "port-forward to svc/$svc failed:" >&2; cat "$log" >&2; exit 1; }
            if [[ "$svc" == postgres ]]; then DB_PORT="$fwd"; else S3_PORT="$fwd"; fi
        done
        DB_HOST=127.0.0.1
        S3_ENDPOINT="127.0.0.1:$S3_PORT"
    fi
    DB_HOST="$DB_HOST" DB_PORT="${DB_PORT:-5432}" DB_USER=postgres DB_PASSWORD=mysecretpassword DB_NAME=postgres \
    S3_ENDPOINT="$S3_ENDPOINT" S3_ACCESS_ID=user S3_SECRET=password S3_BUCKET=test S3_REGION=us-east-1 \
    go test -tags=integration -race -count=1 ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...
    go tool go-test-coverage --config=.testcoverage.yml

# Run web frontend unit tests with Vitest (coverage gate from web/vitest.config.ts)
test-web:
    npm --prefix web ci
    npm --prefix web run test:coverage

# Open a psql shell in this checkout's postgres pod.
psql:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    kubectl --context "$K8S_CONTEXT" -n "$K8S_NAMESPACE" exec -it deploy/postgres -- psql -U postgres -d postgres
