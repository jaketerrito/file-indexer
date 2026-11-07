# Design

Current architecture of file-indexer, written for contributors. This
document describes only what the code does today — ideas not yet
implemented (tagging, video metadata, event-driven indexing, ...) live in
[TODO.md](TODO.md).

## Overview

A searchable web file manager over an S3-compatible bucket. S3 is the
single source of truth for content; Postgres holds a derived search index
that can be rebuilt from the bucket at any time.

| Component | Kind | Role |
| --- | --- | --- |
| `web` | Deployment (HTTP :3000) | TanStack Start SSR app; its server functions act as a BFF over the gRPC services |
| `files` | Deployment (gRPC :50051) | File CRUD: presigned upload/download URLs, listing, deletes |
| `search` | Deployment (gRPC :50051) | Name/key search and content-type filtering over Postgres (ILIKE + pg_trgm word similarity) |
| `index-stat` / `index-preview` / `index-exif` | Deployments | Long-running index workers draining the DB-backed index queue |
| `crawler` | CronJob | Full-bucket scan reconciling the index with S3 |
| `preview-gc` | CronJob | Deletes preview objects that no `index_preview_result` row claims |
| `migrate` | Job | goose migrations from `internal/db/migrations/` |

## Database

Postgres, with goose migrations (`internal/db/migrations/`) and
sqlc-generated queries (`internal/db/queries/` → `internal/db/`). Core
tables: `files` (one row per object) and `directories`; per-index-type
result tables (`index_stat_result`, `index_preview_result`,
`index_exif_result`); and `index_queue`, the work queue scoped by index
type. The `file_infos` view joins files with their results — its metadata
columns are NULL until the corresponding indexer runs.

## Indexing

**Reference-based.** Index requests carry only `(source, s3 key)` — never
file bytes. The indexer GETs the object from S3 and computes all metadata
itself (size, content type, EXIF/XMP, preview image), uniformly across the
API, uploader, and crawler paths. This costs one GET per file, negligible
when the indexer is co-located with storage, and the pull path is needed
anyway for re-indexing and crawler-discovered files.

**Queue-based and self-seeding.** Each index type owns rows in
`index_queue` with lifecycle `pending → claimed → done` (or `error` once
attempts are exhausted), with backoff, claim TTL, and max-attempts policy
in `internal/service/indexer`. Workers seed the queue from `files` on
startup and after draining the queue, re-enqueueing rows whose mark is
older than `files.marked_at`, so edited objects are re-indexed
automatically and a new index type discovers its entire backlog on first
run — the crawler never needs to know which index types exist.

**Upload path.** The browser PUTs bytes directly to S3 via a presigned URL
(multipart for large files), then calls `CommitUpload`, which re-stats the
key in S3 and only then upserts the `files` row — no phantom index entries
from failed uploads. The next queue seed picks the row up.

**Crawler reconciliation.** The crawler lists the bucket, diffs against the
DB, and emits references for unindexed, changed, or removed files, so
out-of-band S3 changes converge. It skips the `INDEX_PREFIX` prefix
(default `.index/`), under which derived objects (previews) are written
back, so derived objects never become `files` rows.

## Observability

The three long-running index workers (`index-stat`, `index-preview`, and
`index-exif`) each expose `/healthz` and `/metrics` on port `8081` (via
`HEALTH_ADDR`, default `:8081`). The `/healthz` endpoint is wired as a
liveness probe so a wedged worker is restarted; `/metrics` currently
reports queue depth and oldest pending item age for Prometheus scraping.
The crawler and `preview-gc` are finite Kubernetes Jobs and deliberately
carry no probes.

## Invariants

- **S3 is the single source of truth.** Nothing is indexed unless it lives
  in S3; the DB is a derived search index, not a store.
- **Idempotent indexing.** Re-indexing the same key is safe (upsert
  semantics); the crawler may re-emit references for unchanged files.
- **Eventual consistency.** The web app may lag the bucket; the crawler
  reconciles.
- **No lost data on write.** Uploads are confirmed against S3 (re-stat
  before indexing) rather than trusted from the client.

Backups are an external concern: protect the bucket (and Postgres) with
storage-level tooling outside this repo.
