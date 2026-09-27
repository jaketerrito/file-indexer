-- +goose Up

-- Add taken_at from the EXIF index to the file_infos read model so the
-- search service can sort by capture date with fallback to S3 last-modified.
-- Postgres cannot add columns to an existing view, so it is dropped and
-- rebuilt.
DROP VIEW IF EXISTS file_infos;
CREATE VIEW file_infos AS
SELECT f.id,
       f.key,
       f.created_at,
       s.content_type,
       s.size_bytes,
       s.last_modified,
       e.taken_at,
       p.preview_key,
       p.width  AS preview_width,
       p.height AS preview_height
FROM files f
LEFT JOIN index_stat_result s ON s.file_id = f.id
LEFT JOIN index_exif_result e ON e.file_id = f.id
LEFT JOIN index_preview_result p ON p.file_id = f.id;

-- The taken_at sort is COALESCE(taken_at, last_modified AT TIME ZONE 'UTC'),
-- which spans the LEFT JOIN between index_exif_result and index_stat_result.
-- A single expression index over that COALESCE is not possible because it
-- crosses tables, but an index on the stat fallback keeps the fallback path
-- from becoming a full scan when taken_at is absent. The existing partial
-- index index_exif_result_taken_at_idx covers the taken_at branch.
CREATE INDEX IF NOT EXISTS index_stat_result_last_modified_utc_idx
    ON index_stat_result ((last_modified AT TIME ZONE 'UTC'), file_id);

-- +goose Down
DROP INDEX IF EXISTS index_stat_result_last_modified_utc_idx;
DROP VIEW IF EXISTS file_infos;
CREATE VIEW file_infos AS
SELECT f.id,
       f.key,
       f.created_at,
       s.content_type,
       s.size_bytes,
       s.last_modified,
       p.preview_key,
       p.width  AS preview_width,
       p.height AS preview_height
FROM files f
LEFT JOIN index_stat_result s ON s.file_id = f.id
LEFT JOIN index_preview_result p ON p.file_id = f.id;
