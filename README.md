# file-indexer

## Database
Postgres Database
Migrations live in `internal/db/migrations/` as SQL files and are handled by [goose](https://github.com/pressly/goose).
SQL queries in `internal/db/queries/` are compiled by [sqlc](https://sqlc.dev) into type-safe Go code in `internal/db/sqlc/`.

## Dev
### Dependencies
- [protoc](https://protobuf.dev/installation)
- [tilt](https://docs.tilt.dev/index.html)
- local k8s cluster ([microk8s](https://docs.tilt.dev/choosing_clusters.html#microk8s))
    - `microk8s enable registry`
    - `microk8s enable hostpath-storage`
    - `microk8s enable dns`

### Commands
protoc and sqldc commands are run automatically via tilt to generate code.

tilt up
tilt down

GRPC_ADDR=:50051 go run -tags proto ./cmd/crawler

