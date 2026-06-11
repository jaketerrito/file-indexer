# file-indexer

## Database
Postgres Database
Migrations live in `internal/db/migrations/` as SQL files and are handled by [goose](https://github.com/pressly/goose).
SQL queries in `internal/db/queries/` are compiled by [sqlc](https://sqlc.dev) into type-safe Go code in `internal/db/sqlc/`.

## Dev
### Dependencies
- [just](https://just.systems/man/en/)
- [psql](https://www.postgresql.org/docs/current/app-psql.html)
- [tilt](https://docs.tilt.dev/index.html)
- [golangci-lint](https://golangci-lint.run)
- local k8s cluster ([microk8s](https://docs.tilt.dev/choosing_clusters.html#microk8s))
    - `microk8s enable registry`
    - `microk8s enable hostpath-storage`
    - `microk8s enable dns`

### Commands
To see available commands: `just`

Start and stop dev servers
`just up`
`just down`

protoc and sqldc commands are run automatically via tilt to generate code.
`GRPC_ADDR=:50051 go run -tags proto ./cmd/crawler`


just file to handle development commads
micro k8s is out of scope of this project, devs can use whatever cluster they want.


### Tilt
Manages development resources in k8s cluster
Automatically generates code
Automatically rebuilds containers
Automatically runs build checks
