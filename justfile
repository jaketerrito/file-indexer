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
    go run github.com/bufbuild/buf/cmd/buf lint proto

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
    go run github.com/bufbuild/buf/cmd/buf format -w proto

# Start local dev environment with Tilt (background)
up:
    tilt up > /dev/null 2>&1 &
    xdg-open http://localhost:10350 2>/dev/null

# Tear down Tilt dev environment and stop the process
down:
    tilt down
    pkill tilt 2>/dev/null; true

# Connect to the project postgres database
psql:
    psql -h localhost -U postgres -d postgres
