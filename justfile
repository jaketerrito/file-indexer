# Show available commands
help:
    @just --list

# Regenerate all generated code (sqlc, protobuf) via go generate
generate:
    go generate ./...

# Run all linters (Go + Kubernetes manifests + protobuf)
lint: lint-go lint-k8s lint-proto

# Run golangci-lint checks on all Go code
lint-go:
    golangci-lint run ./...

# Validate Kubernetes manifests with kubeconform (builds kustomize output first)
lint-k8s:
    kubectl kustomize deploy | kubeconform -strict -summary

# Lint protobuf files with buf
lint-proto:
    go tool buf lint proto

# Auto-format all code (Go + YAML + protobuf)
fmt: fmt-go fmt-yaml fmt-proto

# Auto-format Go code with golangci-lint
fmt-go:
    golangci-lint fmt

# Auto-format YAML files with yamlfmt
fmt-yaml:
    yamlfmt .

# Auto-format protobuf files with buf
fmt-proto:
    go tool buf format -w proto

# Create (or update) the local kind cluster and image registry via ctlptl
cluster-up:
    ctlptl apply -f ctlptl.yaml

# Delete the local kind cluster and image registry
cluster-down:
    ctlptl delete -f ctlptl.yaml

# Start local dev environment with Tilt (background)
up: cluster-up
    tilt up > /dev/null 2>&1 &
    xdg-open http://localhost:10350 2>/dev/null

# Tear down Tilt dev environment and stop the process
down: cluster-down
    tilt down
    pkill tilt 2>/dev/null; true

# Deploy test dependencies (postgres, MinIO, secrets) to the cluster and run
# the full test suite + coverage gate via the test-integration Tilt resource.
# This is what CI runs; reproduce locally with `just cluster-up && just ci`.
ci:
    tilt ci uncategorized postgres local-s3 test-integration

# Run unit tests with race detector and write a coverage profile. Integration
# tests are excluded (build-tag gated); coverage thresholds are only checked by
# test-integration, since unit tests alone cannot reach them.
test:
    go test -race ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...

# Run Go tests with race detector and verbose output
test-verbose:
    go test -race -v ./...

# Run the full test suite (unit + integration) against the local Tilt services
# (postgres on localhost:5432, MinIO on localhost:9000), then check coverage
# thresholds from .testcoverage.yml. Requires `just up` (or equivalent
# port-forwards). Integration tests fail hard if the services are unreachable.
test-integration:
    DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=mysecretpassword DB_NAME=postgres \
    S3_ENDPOINT=localhost:9000 S3_ACCESS_ID=user S3_SECRET=password S3_BUCKET=test \
    go test -tags=integration -race ./... -coverprofile=coverage.out -covermode=atomic -coverpkg=./...
    go tool go-test-coverage --config=.testcoverage.yml

# Connect to the project postgres database
psql:
    psql -h localhost -U postgres -d postgres
