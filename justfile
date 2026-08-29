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

# Ensure the shared dev cluster, the shared image registry, and this
# checkout's namespace all exist. Every checkout shares ONE kind cluster
# ("kind-kind"); isolation is per-namespace — the main checkout deploys to
# "default" (identical to CI), worktrees to "wt-<slug>" (hack/dev-env.sh).
# Creation is strictly additive: an existing cluster or registry is never
# re-applied, so bringing up one checkout can never disturb another
# checkout's running stack. Fails early if this checkout's dev ports are
# taken; override with WORKTREE_INDEX=<n> if two checkouts collide.
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
    # "default" exists in every cluster; applying it only emits a warning.
    if [[ "$K8S_NAMESPACE" != "default" ]]; then
        kubectl --context "$K8S_CONTEXT" create namespace "$K8S_NAMESPACE" \
            --dry-run=client -o yaml | kubectl --context "$K8S_CONTEXT" apply -f - > /dev/null
    fi
    echo "namespace '${K8S_NAMESPACE}' ready on shared cluster (context ${K8S_CONTEXT})"

# Delete this checkout's namespace, taking every resource in it along. The
# shared cluster and registry are left alone for the other checkouts. The
# main checkout uses "default", which k8s forbids deleting, so there
# `tilt-down` alone does the cleanup.
ns-down:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    if [[ "$K8S_NAMESPACE" == "default" ]]; then
        echo "refusing to delete the 'default' namespace; tilt-down already removed this checkout's resources"
        exit 0
    fi
    kubectl --context "$K8S_CONTEXT" delete namespace "$K8S_NAMESPACE" --ignore-not-found

# Destroy the SHARED kind cluster and its registry. This affects EVERY
# checkout, not just this one — every worktree's stack goes with it. For
# per-checkout teardown use `just down`.
cluster-down:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    cluster_yaml="$(mktemp)"
    trap 'rm -f "$cluster_yaml"' EXIT
    printf 'apiVersion: ctlptl.dev/v1alpha1\nkind: Cluster\nproduct: kind\nname: %s\nregistry: ctlptl-registry\n' "$CLUSTER_NAME" > "$cluster_yaml"
    ctlptl delete -f "$cluster_yaml" || true

# Start Tilt dev environment (background) in this checkout's namespace.
# Worktrees get their own Tilt UI port (10350+N, see hack/dev-env.sh).
tilt-up:
    #!/usr/bin/env bash
    set -euo pipefail
    hack/dev-env.sh --check > /dev/null
    eval "$(hack/dev-env.sh)"
    nohup tilt up --context "$K8S_CONTEXT" --namespace "$K8S_NAMESPACE" --port "$TILT_PORT" > /dev/null 2>&1 &
    echo $! > .tilt.pid
    xdg-open "http://localhost:$TILT_PORT" 2>/dev/null || true

# Tear down Tilt dev environment for this checkout only (leaves other
# checkouts' Tilt instances and namespaces running).
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

# Ensure cluster + namespace, then start Tilt
up: cluster-up tilt-up

# Stop Tilt and delete this checkout's namespace (shared cluster survives)
down: tilt-down ns-down

# Deploy everything with auto_init=True (all services, postgres, MinIO,
# secrets, the lint local resource) and run the full test suite + coverage
# gate via the test-integration Tilt resource. Verifies real rollouts of every
# service, not just manifest validity. This is what CI runs; reproduce locally
# with `just cluster-up && just ci`.
ci:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    tilt ci --context "$K8S_CONTEXT" --namespace "$K8S_NAMESPACE" --port "$TILT_PORT"

# Run unit tests with race detector and write a coverage profile. Integration
# tests are excluded (build-tag gated); coverage thresholds are only checked by
# test-integration, since unit tests alone cannot reach them.
test:
    go test -race ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...

# Run Go tests with race detector and verbose output
test-verbose:
    go test -race -v ./...

# Run the full test suite (unit + integration) against the local Tilt services
# (postgres on localhost:$DB_PORT, MinIO on localhost:$S3_PORT), then check
# coverage thresholds from .testcoverage.yml. Requires `just up` (or equivalent
# port-forwards). Integration tests fail hard if the services are unreachable.
test-integration:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    DB_HOST=localhost DB_PORT="$DB_PORT" DB_USER=postgres DB_PASSWORD=mysecretpassword DB_NAME=postgres \
    S3_ENDPOINT="localhost:$S3_PORT" S3_ACCESS_ID=user S3_SECRET=password S3_BUCKET=test S3_REGION=us-east-1 \
    go test -tags=integration -race -count=1 ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...
    go tool go-test-coverage --config=.testcoverage.yml

# Run web frontend unit tests with Vitest (coverage gate from web/vitest.config.ts)
test-web:
    npm --prefix web ci
    npm --prefix web run test:coverage

# Connect to the project postgres database (respects per-checkout port).
psql:
    #!/usr/bin/env bash
    set -euo pipefail
    eval "$(hack/dev-env.sh)"
    psql -h localhost -p "$DB_PORT" -U postgres -d postgres
