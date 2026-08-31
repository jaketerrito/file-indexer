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

# Validate Kubernetes manifests with kubeconform (builds kustomize output
# first). deploy/routes.yaml is NOT in the kustomization: its `$checkout`
# placeholders can never validate against the HTTPRoute hostname schema —
# it's rendered by `tract render` in the Tiltfile instead (same exemption
# the Tiltfile-blob routes always had).
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

# Verify the shared cluster, ensure this checkout's namespace, then start
# Tilt in the background (tract derives the per-checkout env; the shared cluster is prebuilt — see the tract README).
up:
    tract up --detach

# Stop Tilt and delete this checkout's namespace (shared cluster survives)
down:
    tract down

# Deploy everything with auto_init=True (all services, postgres, MinIO,
# secrets, the lint local resource) and run the full test suite + coverage
# gate via the test-integration Tilt resource. Verifies real rollouts of every
# service, not just manifest validity. This is what CI runs; reproduce locally
# with `just ci` (the shared cluster must already exist).
ci:
    tract tilt ci

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
    eval "$(tract env)"
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
    eval "$(tract env)"
    kubectl --context "$K8S_CONTEXT" -n "$K8S_NAMESPACE" exec -it deploy/postgres -- psql -U postgres -d postgres
