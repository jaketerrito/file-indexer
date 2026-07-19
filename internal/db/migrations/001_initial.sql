-- +goose Up

-- files is pure identity: which objects exist and when they were last seen
-- with what content. The crawler writes marked_at = the object's S3
-- last-modified time on every listing; index runs compare their stored mark
-- against files.marked_at to determine whether their results are stale.
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

-- index_queue is the shared job queue for every index type: one row per
-- (index_type, file) tracking where that file is in that index type's
-- lifecycle. Index types never define their own queue table or queries —
-- they only provide a processing function and a result table. Results live
-- separately (see index_stat_result below) so a queue row carries no
-- type-specific columns.
--
-- Lifecycle per (index_type, file_id):
--
--   (no row)  --seed-->  pending  --claim-->  claimed  --complete-->  done
--                          ^                    |
--                          +------fail----------+--(attempts exhausted)--> error
--
-- Staleness detection: the seed step also re-enqueues done rows whose
-- stored mark no longer matches files.marked_at, so an edited object is
-- automatically re-indexed on the next seed without the crawler needing to
-- know which index types exist.
--
-- Timing policy (backoff, claim TTL, max attempts) lives in the runner
-- package; these queries only persist the decisions it makes.
CREATE TABLE IF NOT EXISTS index_queue (
    index_type      TEXT NOT NULL,
    file_id         BIGINT NOT NULL REFERENCES files (id) ON DELETE CASCADE,

    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'done', 'error')),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    last_error      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The files.marked_at value this row's result corresponds to. NULL
    -- until the first successful run. The seed step re-enqueues done rows
    -- where mark IS DISTINCT FROM files.marked_at, making stale detection
    -- trivial and index-type-agnostic.
    mark            TIMESTAMPTZ,

    PRIMARY KEY (index_type, file_id)
);

-- Partial indexes keep the poll query an index-only scan even when the
-- table is dominated by done rows.
CREATE INDEX IF NOT EXISTS index_queue_pending_idx ON index_queue (index_type, next_attempt_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS index_queue_claimed_idx ON index_queue (index_type, claimed_at) WHERE status = 'claimed';

-- index_stat_result holds the stat index type's output (S3 StatObject
-- metadata), one row per successfully indexed file. A row only exists once
-- the index_queue('stat', file_id) row has completed at least once; result
-- columns are therefore NOT NULL. Each index type gets its own such table
-- so pipelines' results have independent lifecycles and schemas.
CREATE TABLE IF NOT EXISTS index_stat_result (
    file_id       BIGINT PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,
    content_type  TEXT NOT NULL,
    size_bytes    BIGINT NOT NULL,
    last_modified TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Keyset pagination sort indexes for the search service. The expressions
-- match file_infos' COALESCE (the view's LEFT JOIN makes these columns
-- nullable even though they're NOT NULL in this table) so the planner can
-- use them for an index-only scan.
CREATE INDEX IF NOT EXISTS index_stat_result_last_modified_idx ON index_stat_result ((COALESCE(last_modified, 'epoch'::timestamptz)), file_id);
CREATE INDEX IF NOT EXISTS index_stat_result_size_bytes_idx ON index_stat_result ((COALESCE(size_bytes, 0)), file_id);

-- file_infos is the read model the files/search services serve from.
CREATE VIEW file_infos AS
SELECT f.id,
       f.key,
       f.created_at,
       s.content_type,
       s.size_bytes,
       s.last_modified
FROM files f
LEFT JOIN index_stat_result s ON s.file_id = f.id;

-- +goose Down
DROP VIEW IF EXISTS file_infos;
DROP TABLE IF EXISTS index_stat_result;
DROP TABLE IF EXISTS index_queue;
DROP TABLE IF EXISTS files;
