-- +goose Up
-- Support prefix listing (LIKE 'prefix%') regardless of database collation.
CREATE INDEX IF NOT EXISTS files_key_pattern_idx ON files (key text_pattern_ops);
-- Keyset pagination indexes; id is the tie-breaker in every sort.
CREATE INDEX IF NOT EXISTS files_created_at_id_idx ON files (created_at, id);
-- size_bytes is nullable; list queries sort on COALESCE(size_bytes, 0).
CREATE INDEX IF NOT EXISTS files_size_bytes_id_idx ON files ((COALESCE(size_bytes, 0)), id);

-- +goose Down
DROP INDEX IF EXISTS files_size_bytes_id_idx;
DROP INDEX IF EXISTS files_created_at_id_idx;
DROP INDEX IF EXISTS files_key_pattern_idx;
