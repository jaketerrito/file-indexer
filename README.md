# file-indexer
[![trivy](https://github.com/jaketerrito/file-indexer/actions/workflows/trivy.yml/badge.svg)](https://github.com/jaketerrito/file-indexer/actions/workflows/trivy.yml)

## Components
### Stat
Builds a searchable treasure map from basic file booty metadata (content type, size, last modified) in the s3 bucket hold

### Preview
Generates downscaled preview images fer image files, written back to the same s3 bucket under a
configurable key prefix (`INDEX_PREFIX`) that the crawler be ignorin'

### Exif
Extracts EXIF and XMP metadata (camera make/model, capture time, exposure settings, GPS, XMP
title/rating/keywords) from image and camera-RAW files (JPEG, TIFF, PNG, HEIC/HEIF/AVIF,
CR2/CR3/CRW, DNG, NEF, ARW, RW2). Files with neither EXIF nor XMP be marked done with no result
row, arrr.

### Crawler
Searches fer new files in the s3 bucket, sendin' 'em to the index workers (index-stat, index-preview, index-exif)

### File Manager
Crud operations on files, ye scallywag

### Database
Postgres Database, the ship's logbook
Migrations be livin' in `internal/db/migrations/` as SQL files and be handled by [goose](https://github.com/pressly/goose).
SQL queries in `internal/db/queries/` be compiled by [sqlc](https://sqlc.dev) into type-safe Go code in `internal/db/`.

## Dev
### Dependencies
- [just](https://just.systems/man/en/)
- [psql](https://www.postgresql.org/docs/current/app-psql.html)
- [tilt](https://docs.tilt.dev/index.html)
- [golangci-lint](https://golangci-lint.run)
- [docker](https://docs.docker.com/engine/install/)
- [kind](https://kind.sigs.k8s.io)
- [ctlptl](https://github.com/tilt-dev/ctlptl)
- [Node.js](https://nodejs.org) >= 24 (current LTS; npm ships with it; used fer the web frontend)

### Components
- kind cluster provisioned with ctlptl
- tilt
  - Manages development resources in yer k8s cluster
  - Automatically generates code, like magic
  - Automatically rebuilds containers, keepin' the ship seaworthy

### Gettin' started
Run `just` to see all available commands at yer disposal.

`just up` — creates (once per vessel) the shared local kind cluster, then uses Tilt to provision this checkout's resources into its own namespace (named after the checkout directory), runs DB migrations, builds and deploys the app containers (index-stat, index-preview, files), and keeps 'em live-reloadin' on code changes. Code generation (sqlc, protobuf) runs automatically, no sweat. Prints this checkout's Tilt web UI URL (a stable per-checkout port).

Every checkout o' this repo shares the one cluster and can run at the same time: services be reached via the shared gateway at `http://<svc>.<checkout-dir>.localhost` — the app at `http://web.<checkout-dir>.localhost`, the MinIO console at `http://s3-console.<checkout-dir>.localhost`. `just down` tears down only this checkout's namespace; `just cluster-down` destroys the SHARED cluster and every checkout's stack with it, sendin' 'em all to Davy Jones' locker.

The MinIO bucket comes pre-seeded with a small sample dataset (see
`deploy/overlays/local/seed/`) — a few EXIF-bearin' photos and text files. The crawler be
manual-trigger in Tilt (click it in the web UI) since it ain't somethin' ye
want runnin' on every code change; trigger it once to index the seed data.

### Testin' the waters
`just test` runs unit tests only, matey

Run the full suite (unit + integration) and check coverage thresholds from
`.testcoverage.yml`:
`just test-integration`

Services come from explicit `DB_HOST`/`S3_ENDPOINT` env (what CI sets) or, when
unset, from this checkout's namespace — postgres via an ephemeral kubectl
port-forward, MinIO via the shared gateway (requires `just up`).

Integration tests fail hard if postgres/MinIO be unreachable; they never skip, no quarter given.

### E2E tests

Playwright e2e tests be livin' in `web/e2e/` (config: `web/playwright.config.ts`)
and run against this checkout's Tilt deployment through the shared gateway —
they need `just up` runnin', with the seed dataset indexed (the crawler runs
once when the session starts):

`just test-e2e`

The recipe installs the Playwright Chromium binary on first run and points the
suite at `http://web.<checkout-dir>.localhost`. The suite also runs as the
`test-e2e` Tilt resource under `just ci` (this be the only place it runs in
CI, via the deploy-verify workflow).

### CI cachin'

`setup-go`'s built-in cachin' be disabled in CI; every Go job instead uses the `.github/actions/setup-go-cache` composite action, which layers two `actions/cache` entries:
- **Module cache** (`~/go/pkg/mod`), keyed only on a hash o' `go.sum`. Content-addressed and identical across every job, so it be shared — no poisonin' risk, savvy?
- **Build cache** (`~/.cache/go-build`), keyed on a per-job `cache-key-prefix` input. This one ain't content-addressed (it be keyed on package import graphs, which differ per job, e.g. `-tags=integration`), so it stays job-scoped.

#### Conventions
- **Table-driven tests with standard `testing` package, ye landlubber.** No external assertion libraries.
- **Consumer-side interfaces, mockery-generated fakes.** Each service defines a narrow interface (e.g. `ObjectStore`, `FileIndex`) listin' only what it uses. Mocks be generated by [mockery](https://vektra.github.io/mockery/) (config in `.mockery.yaml`, run via `go generate ./...` / `just generate`) into `mocks_test.go` colocated with the interface's consumer.
- **Integration tests** use `//go:build integration` and be excluded from `go test ./...`. Only add 'em when ye need real DB or S3 interactions, arrr.
