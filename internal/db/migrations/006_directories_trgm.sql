-- +goose Up

-- Serves SearchDirectories' ILIKE '%q%' substring branch and path %> q
-- word-similarity branch on directories — the directories counterpart of
-- files_key_trgm_idx (005_pg_trgm.sql).
CREATE INDEX IF NOT EXISTS directories_path_trgm_idx ON directories USING gin (path gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS directories_path_trgm_idx;
