# Show available commands
help:
    @just --list

# This checkout's Kubernetes namespace: its (sanitized) directory name. Every
# checkout of this repo deploys into its own namespace on the shared kind
# cluster; services are at http://<svc>.<ns>.localhost via the shared gateway.
ns := `basename "$(git rev-parse --show-toplevel)" | tr 'A-Z' 'a-z' | sed -E -e 's/[^a-z0-9-]/-/g' -e 's/-+/-/g' -e 's/^-|-$//g' | cut -c1-63`

# Stable per-checkout Tilt UI port (10351-10450): tilt has no auto-increment, so
# concurrent checkouts would collide on the default 10350. Hashes the same
# derivation as `ns`, inlined: just does NOT interpolate {{ns}} inside a
# backtick expression (it would hash the literal string, a constant).
tilt_port := `basename "$(git rev-parse --show-toplevel)" | tr 'A-Z' 'a-z' | sed -E -e 's/[^a-z0-9-]/-/g' -e 's/-+/-/g' -e 's/^-|-$//g' | cut -c1-63 | cksum | cut -d' ' -f1 | awk '{ print 10351 + $1 % 100 }'`

# Regenerate all generated code (sqlc, protobuf, web TS clients). The web
# codegen requires npm: protoc-gen-es comes from web/node_modules. Run this
# after touching migrations, sqlc queries, .proto files, or interfaces listed
# in .mockery.yaml, and commit the regenerated output.
generate:
    go generate ./...
    npm --prefix web ci
    go tool buf generate --template proto/buf.gen.web.yaml proto --include-imports

# Run all linters (Go + Kubernetes manifests + protobuf + web TS)
lint: lint-go lint-k8s lint-proto lint-web

# Run golangci-lint checks on all Go code
lint-go:
    go tool golangci-lint run ./...

# Validate Kubernetes manifests with kubeconform (builds kustomize output first)
lint-k8s:
    go tool kustomize build deploy/base | go tool kubeconform -strict -summary

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

# Create the shared dev cluster (one-time per machine, safe to re-run). Prereqs:
# docker, kubectl, ctlptl.
cluster-up:
    cluster/up.sh
    @echo "cluster ready; this checkout's URLs: http://<svc>.{{ns}}.localhost (svc: web, s3, s3-console)"

# Destroy the SHARED cluster and every checkout's stack with it.
cluster-down:
    cluster/down.sh

# Start this checkout's Tilt dev environment (background), deploying into the
# checkout's own namespace (created by the Tiltfile on first load).
tilt-up:
    tilt up --namespace {{ns}} --port {{tilt_port}} > /dev/null 2>&1 &
    @echo "tilt UI: http://localhost:{{tilt_port}}"
    @echo "web: http://web.{{ns}}.localhost (also: s3.{{ns}}.localhost, s3-console.{{ns}}.localhost)"

# Stop this checkout's Tilt and delete its namespace (the shared cluster and
# other checkouts survive). The pkill targets only this checkout's Tilt process
# (matched by its namespace, not the UI port — cksum%100 ports can collide
# across checkouts): a still-running session would re-apply everything
# `tilt down` deletes.
tilt-down:
    pkill -f "[t]ilt up --namespace {{ns}} --port" || true # [t] bracket: don't match this recipe's own shell
    tilt down --namespace {{ns}} --delete-namespaces

# Create the shared cluster (if needed) and start this checkout's Tilt
up: cluster-up tilt-up

# Stop this checkout's Tilt and delete its namespace (`just cluster-down`
# destroys the SHARED cluster and every checkout's stack)
down: tilt-down

# Deploy everything with auto_init=True (all services, postgres, MinIO,
# secrets, the lint local resource) and run the full test suite + coverage
# gate via the test-integration Tilt resource. Verifies real rollouts of every
# service, not just manifest validity. This is what CI runs; reproduce locally
# with `just cluster-up && just ci`.
ci:
    tilt ci --namespace {{ns}} --port {{tilt_port}}

# Run unit tests with race detector and write a coverage profile. Integration
# tests are excluded (build-tag gated); coverage thresholds are only checked by
# test-integration, since unit tests alone cannot reach them.
test:
    go test -race ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...

# Run Go tests with race detector and verbose output
test-verbose:
    go test -race -v ./...

# Run the full test suite (unit + integration), then check coverage thresholds
# from .testcoverage.yml. Integration tests fail hard if services are unreachable.
test-integration:
    #!/usr/bin/env bash
    # Services come from explicit DB_HOST/S3_ENDPOINT env (CI, against
    # runner-local containers) or, when unset, from this checkout's namespace:
    # postgres via an ephemeral kubectl port-forward, S3 via the shared gateway
    # (requires `just up`).
    set -euo pipefail
    ns="{{ns}}"
    pf_pid=""
    trap '[[ -z "$pf_pid" ]] || kill "$pf_pid" 2>/dev/null || true' EXIT
    if [[ -z "${DB_HOST:-}" ]]; then
        log="$(mktemp)"
        kubectl --context kind-kind -n "$ns" port-forward svc/postgres ":5432" > "$log" 2>&1 &
        pf_pid=$!
        for _ in $(seq 1 50); do grep -q "Forwarding from" "$log" 2>/dev/null && break; sleep 0.2; done
        DB_PORT="$(grep -m1 -oE '127\.0\.0\.1:[0-9]+' "$log" | cut -d: -f2 || true)"
        [[ -n "$DB_PORT" ]] || { echo "port-forward to svc/postgres failed:" >&2; cat "$log" >&2; exit 1; }
        DB_HOST=127.0.0.1
    fi
    DB_HOST="$DB_HOST" DB_PORT="${DB_PORT:-5432}" DB_USER=postgres DB_PASSWORD=mysecretpassword DB_NAME=postgres \
    S3_ENDPOINT="${S3_ENDPOINT:-s3.$ns.localhost}" S3_ACCESS_ID=user S3_SECRET=password S3_BUCKET=test S3_REGION=us-east-1 \
    go test -tags=integration -race -count=1 ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...
    go tool go-test-coverage --config=.testcoverage.yml

# Run web frontend unit tests with Vitest (coverage gate from web/vitest.config.ts)
test-web:
    npm --prefix web ci
    npm --prefix web run test:coverage

# Open a psql shell in this checkout's postgres pod
psql:
    kubectl --context kind-kind -n {{ns}} exec -it deploy/postgres -- psql -U postgres -d postgres
