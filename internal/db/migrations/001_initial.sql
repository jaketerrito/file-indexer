-- +goose Up

-- files is pure identity: which objects exist and when they were last seen
-- with what content. The crawler writes marked_at = the object's S3
-- last-modified time on every listing; index workers compare their stored
-- mark against files.marked_at to determine whether their results are stale.
-- created_at is discovery time, not object mtime.
CREATE TABLE IF NOT EXISTS files (
    id         BIGSERIAL PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Last-modified time from the most recent S3 listing. Bumped on re-crawl
    -- only when the listing shows a newer mtime (GREATEST), so idempotent
    -- re-crawls produce no change for unchanged objects.
    marked_at  TIMESTAMPTZ NOT NULL
);

-- Support prefix listing (LIKE 'prefix%') regardless of database collation.
CREATE INDEX IF NOT EXISTS files_key_pattern_idx ON files (key text_pattern_ops);

-- index_stat is the stat index type: queue state for the worker pool plus
-- the metadata that indexing produces, one row per file. Each index type
-- gets its own such table so pipelines have independent lifecycles.
CREATE TABLE IF NOT EXISTS index_stat (
    file_id         BIGINT PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,

    -- Queue state.
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'done', 'error')),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    last_error      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The files.marked_at value this row's results correspond to. NULL until
    -- the first successful run. The seed step re-enqueues done rows where
    -- mark IS DISTINCT FROM files.marked_at, making stale detection trivial
    -- and index-type-agnostic.
    mark            TIMESTAMPTZ,

    -- Stat results (S3 StatObject).
    content_type    TEXT,
    size_bytes      BIGINT,
    last_modified   TIMESTAMPTZ
);

-- Partial indexes keep the poll query an index-only scan even when the
-- table is dominated by done rows.
CREATE INDEX IF NOT EXISTS index_stat_pending_idx ON index_stat (next_attempt_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS index_stat_claimed_idx ON index_stat (claimed_at) WHERE status = 'claimed';

-- Keyset pagination sort indexes for the search service.
CREATE INDEX IF NOT EXISTS index_stat_last_modified_idx ON index_stat ((COALESCE(last_modified, 'epoch'::timestamptz)), file_id);
CREATE INDEX IF NOT EXISTS index_stat_size_bytes_idx ON index_stat ((COALESCE(size_bytes, 0)), file_id);

-- file_infos is the read model the files/search services serve from.
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
