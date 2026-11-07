# file-indexer
[![trivy](https://github.com/jaketerrito/file-indexer/actions/workflows/trivy.yml/badge.svg)](https://github.com/jaketerrito/file-indexer/actions/workflows/trivy.yml)

A searchable web file manager for an S3-compatible bucket. The bucket is the
single source of truth: file-indexer crawls it and builds a Postgres-backed
search index (file metadata, EXIF, preview thumbnails) so you can browse,
search, and manage your files from a web UI.

## Features

- Browse, upload, and delete files — uploads go straight to the bucket via
  presigned URLs
- Search files by name, with fuzzy matching and content-type filtering
- EXIF/XMP metadata extraction for photos and camera RAW files (JPEG, TIFF,
  PNG, HEIC/HEIF/AVIF, CR2/CR3/CRW, DNG, NEF, ARW, RW2)
- Auto-generated preview thumbnails for images
- Background crawler that reconciles the index with the bucket

<!-- TODO: screenshot or short demo recording -->

## Quickstart (local dev)

Requires [just](https://just.systems/man/en/), [docker](https://docs.docker.com/engine/install/),
[kind](https://kind.sigs.k8s.io), [ctlptl](https://github.com/tilt-dev/ctlptl),
and [tilt](https://docs.tilt.dev/). See [CONTRIBUTING.md](CONTRIBUTING.md) for
details.

1. `just up` — creates the shared local kind cluster (once per machine), then
   deploys this checkout into its own namespace, named after the checkout
   directory.
2. Open `http://web.<checkout-dir>.localhost`.
3. Trigger the `crawler` resource once in the Tilt UI to index the pre-seeded
   sample data.

`just` lists all available commands. `just down` tears down only this
checkout's namespace; beware that `just cluster-down` destroys the SHARED
cluster and every checkout's stack with it.

## Usage

<!-- TODO: short usage guide — search, photos view, uploads, metadata modal -->

## Documentation

- [docs/deployment.md](docs/deployment.md) — deploying to a real environment
- [docs/DESIGN.md](docs/DESIGN.md) — architecture and design rationale
- [CONTRIBUTING.md](CONTRIBUTING.md) — development setup, testing, and conventions
- [CHANGELOG.md](CHANGELOG.md)

## License

TODO: a license has not been chosen yet.
