-- +goose Up
-- Per-index-type status table for the stat indexer worker pool. Each index
-- type (stat, exif, previews, ...) gets its own table so pipelines have
-- independent lifecycles and never contend on a shared status column. A files
-- row without an index_stat row is "undiscovered"; workers seed missing rows
-- themselves (see SeedIndexStat).
CREATE TABLE IF NOT EXISTS index_stat (
    file_id         BIGINT PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'done', 'error')),
    attempts        INT NOT NULL DEFAULT 0,
    -- Earliest time a pending row may be claimed; enables retry backoff.
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    last_error      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Partial indexes keep the poll query an index-only scan even when the table
-- is dominated by done rows.
CREATE INDEX IF NOT EXISTS index_stat_pending_idx ON index_stat (next_attempt_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS index_stat_claimed_idx ON index_stat (claimed_at) WHERE status = 'claimed';

-- +goose Down
DROP TABLE IF EXISTS index_stat;
