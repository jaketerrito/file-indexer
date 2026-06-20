# file-indexer
[![trivy](https://github.com/jaketerrito/file-indexer/actions/workflows/trivy.yml/badge.svg)](https://github.com/jaketerrito/file-indexer/actions/workflows/trivy.yml)

## Components
### Indexer
Builds searchable database from files in s3 bucket

### Crawler
Searches for new files in s3 bucket, sending to indexer

### File Manager
Crud operations on files

### Database
Postgres Database
Migrations live in `internal/db/migrations/` as SQL files and are handled by [goose](https://github.com/pressly/goose).
SQL queries in `internal/db/queries/` are compiled by [sqlc](https://sqlc.dev) into type-safe Go code in `internal/db/`.

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

crawler gets run through tilt too now

just file to handle development commads
micro k8s is out of scope of this project, devs can use whatever cluster they want.


### Tilt
Manages development resources in k8s cluster
Automatically generates code
Automatically rebuilds containers

### Testing

Run all unit tests:
`just test`

Run with race detector + verbose output:
`just test-verbose`

Run with coverage report:
`just test-cover`

Run integration tests (requires DB/S3):
`just test-integration`

#### Conventions

- **Table-driven tests with standard `testing` package.** No external assertion libraries.
- **Consumer-side interfaces for fakes.** Each service defines a narrow `Store` interface listing only the queries it uses. Tests pass hand-written `fakeStore`/`fakeStorage` structs; no mocking framework needed.
- **Integration tests** use `//go:build integration` and are excluded from `go test ./...`. Only add them when you need real DB or S3 interactions.
Automatically runs build checks
