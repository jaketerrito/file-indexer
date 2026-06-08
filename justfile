# Show available commands
help:
    @just --list

# Connect to the project postgres database
psql:
    psql -h localhost -U postgres -d postgres

# Generate sqlc type-safe Go code from SQL queries
sqlc:
    sqlc generate

# Compile protobuf definitions into Go code
proto:
    protoc --proto_path=proto --go_out=internal/pb --go_opt=paths=source_relative --go-grpc_out=internal/pb --go-grpc_opt=paths=source_relative proto/*.proto

# Run golangci-lint checks on all Go code
lint:
    golangci-lint run ./...

# Auto-format Go code with golangci-lint
fmt:
    golangci-lint fmt
