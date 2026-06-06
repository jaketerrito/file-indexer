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

protoc --proto_path=proto \
  --go_out=internal/pb --go_opt=paths=source_relative \
  --go-grpc_out=internal/pb --go-grpc_opt=paths=source_relative \
  proto/*.proto

docker build --build-arg BUILD_TARGET="./cmd/indexer" -t indexer .
docker run -p 50051:50051 -e GRPC_ADDR=:50051 --rm -it indexer

tilt up
tilt down

GRPC_ADDR=:50051 go run -tags proto ./cmd/crawler

