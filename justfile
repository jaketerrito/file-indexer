# Show available commands
help:
    @just --list

# Regenerate all generated code (sqlc, protobuf) via go generate
generate:
    go generate ./...

# Run golangci-lint checks on all Go code
lint:
    golangci-lint run ./...

# Auto-format Go code with golangci-lint
fmt:
    golangci-lint fmt

# Start local dev environment with Tilt (background)
up:
    nohup tilt up > /dev/null 2>&1 &

# Tear down Tilt dev environment and stop the process
down:
    tilt down
    pkill tilt 2>/dev/null; true

# Connect to the project postgres database
psql:
    psql -h localhost -U postgres -d postgres
