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
- [docker](https://docs.docker.com/engine/install/)
- [kind](https://kind.sigs.k8s.io)
- [ctlptl](https://github.com/tilt-dev/ctlptl)
- [Node.js](https://nodejs.org) >= 24 (current LTS; npm ships with it; used for the web frontend)

### Components
- kind cluster provisioned with ctlptl
- tilt
  - Manages development resources in k8s cluster
  - Automatically generates code
  - Automatically rebuilds containers

### Getting started
Run `just` to see all available commands.

`just up` — Launches local kind cluster and usese Tilt to provision resources, runs DB migrations, builds and deploys the app containers (indexer, files), and keeps them live-reloading on code changes. Code generation (sqlc, protobuf) runs automatically. Opens the Tilt web UI at http://localhost:10350.

### Testing
`just test` runs unit tests only

Run the full suite (unit + integration, requires the Tilt dev environment) and
check coverage thresholds from `.testcoverage.yml`:
`just test-integration`

Integration tests fail hard if postgres/MinIO are unreachable; they never skip.

### CI caching

`setup-go`'s built-in caching is disabled in CI because its cache key (a hash of `go.sum`) is shared across all Go jobs. Jobs with different module or build-cache needs end up poisoning each other's caches. Instead, each job uses `actions/cache` with its own unique key prefix.

#### Conventions
- **Table-driven tests with standard `testing` package.** No external assertion libraries.
- **Consumer-side interfaces for fakes.** Each service defines a narrow `Store` interface listing only the queries it uses. Tests pass hand-written `fakeStore`/`fakeStorage` structs; no mocking framework needed.
- **Integration tests** use `//go:build integration` and are excluded from `go test ./...`. Only add them when you need real DB or S3 interactions.
