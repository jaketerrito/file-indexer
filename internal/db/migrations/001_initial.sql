-- +goose Up

-- files is pure identity: which objects exist. The crawler (and later the
-- upload API) only ever writes the key here; everything the object *is*
-- (content type, size, mtime, ...) is computed by indexers and lives in the
-- per-index-type tables below. created_at is discovery time, not object
-- mtime.
CREATE TABLE IF NOT EXISTS files (
    id         BIGSERIAL PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Support prefix listing (LIKE 'prefix%') regardless of database collation.
CREATE INDEX IF NOT EXISTS files_key_pattern_idx ON files (key text_pattern_ops);

-- index_stat is the stat index type: queue state for the worker pool plus
-- the metadata that indexing produces, one row per file. Each index type
-- (stat, exif, previews, ...) gets its own such table so pipelines have
-- independent lifecycles and never contend on a shared status column. A
-- files row without an index_stat row is "undiscovered"; workers seed
-- missing rows themselves (see SeedIndexStat).
--
-- Queue state and results share the table deliberately: completing a job is
-- then a single atomic UPDATE (status + metadata), and the metadata columns
-- are simply NULL until the first successful run.
CREATE TABLE IF NOT EXISTS index_stat (
    file_id         BIGINT PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,

    -- Queue state. next_attempt_at is the earliest time a pending row may
    -- be claimed; it implements retry backoff.
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'done', 'error')),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    last_error      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Stat results (S3 StatObject). last_modified is stored truncated to
    -- whole seconds: the HTTP Last-Modified header only has second
    -- precision, while bucket listings carry sub-second precision, and the
    -- crawler's change detection compares the two.
    content_type    TEXT,
    size_bytes      BIGINT,
    last_modified   TIMESTAMPTZ
);

-- Partial indexes keep the poll query an index-only scan even when the
-- table is dominated by done rows.
CREATE INDEX IF NOT EXISTS index_stat_pending_idx ON index_stat (next_attempt_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS index_stat_claimed_idx ON index_stat (claimed_at) WHERE status = 'claimed';

-- Keyset pagination sort indexes for the search service; file_id is the
-- tie-breaker in every sort. NULL metadata (not yet indexed) sorts as the
-- COALESCE fallback used by the list queries.
CREATE INDEX IF NOT EXISTS index_stat_last_modified_idx ON index_stat ((COALESCE(last_modified, 'epoch'::timestamptz)), file_id);
CREATE INDEX IF NOT EXISTS index_stat_size_bytes_idx ON index_stat ((COALESCE(size_bytes, 0)), file_id);

-- file_infos is the read model the files/search services serve from:
-- identity joined with stat metadata. Files not yet stat-indexed appear
-- with NULL metadata.
CREATE VIEW file_infos AS
SELECT f.id,
       f.key,
       f.created_at,
       s.content_type,
       s.size_bytes,
       s.last_modified
FROM files f
LEFT JOIN index_stat s ON s.file_id = f.id;

-- +goose Down
DROP VIEW IF EXISTS file_infos;
DROP TABLE IF EXISTS index_stat;
DROP TABLE IF EXISTS files;
