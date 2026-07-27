-- +goose Up

-- index_preview_result holds the preview index type's output: one row per
-- file for which a downscaled preview image was generated and written back
-- to object storage under the configured index prefix. Files that are not
-- images, are too large, or whose bytes could not be decoded are marked done
-- in index_queue with no row here, so an absent row means "no preview
-- exists", not "not yet processed".
CREATE TABLE IF NOT EXISTS index_preview_result (
    file_id     BIGINT PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,
    -- Object key of the generated preview, in the same bucket as the source.
    -- Extensionless on purpose; the format lives in the object's Content-Type.
    preview_key TEXT NOT NULL,
    -- Dimensions of the preview, not of the source. Aspect ratio is preserved
    -- and sources are never upscaled, so these vary per file; the frontend
    -- uses them to reserve layout space and avoid shift as images load.
    width       INT NOT NULL,
    height      INT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Postgres cannot add columns to an existing view, so file_infos is dropped
-- and rebuilt. The new join is on a primary key and leaves the keyset
-- pagination sort indexes from 001 usable.
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

-- +goose Down
DROP VIEW IF EXISTS file_infos;
CREATE VIEW file_infos AS
SELECT f.id,
       f.key,
       f.created_at,
       s.content_type,
       s.size_bytes,
       s.last_modified
FROM files f
LEFT JOIN index_stat_result s ON s.file_id = f.id;
DROP TABLE IF EXISTS index_preview_result;
