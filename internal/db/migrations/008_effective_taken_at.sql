-- +goose Up
-- +goose StatementBegin

-- Denormalize the taken_at sort value onto files so it can be indexed and
-- the search service no longer has to sort the full cross-table join for
-- every page. The value is kept in sync by triggers on the source tables.
ALTER TABLE files
    ADD COLUMN effective_taken_at TIMESTAMP NOT NULL DEFAULT 'epoch'::timestamp;

CREATE INDEX IF NOT EXISTS files_effective_taken_at_idx
    ON files (effective_taken_at, id);

CREATE OR REPLACE FUNCTION update_file_effective_taken_at(p_file_id BIGINT)
RETURNS VOID AS $$
BEGIN
    UPDATE files
    SET effective_taken_at = COALESCE(
        (SELECT e.taken_at FROM index_exif_result e WHERE e.file_id = p_file_id),
        (SELECT s.last_modified AT TIME ZONE 'UTC' FROM index_stat_result s WHERE s.file_id = p_file_id),
        'epoch'::timestamp
    )
    WHERE files.id = p_file_id;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION index_exif_result_effective_taken_at_trigger()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM update_file_effective_taken_at(COALESCE(NEW.file_id, OLD.file_id));
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION index_stat_result_effective_taken_at_trigger()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM update_file_effective_taken_at(COALESCE(NEW.file_id, OLD.file_id));
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER index_exif_result_effective_taken_at_trigger
    AFTER INSERT OR UPDATE OR DELETE ON index_exif_result
    FOR EACH ROW
    EXECUTE FUNCTION index_exif_result_effective_taken_at_trigger();

CREATE TRIGGER index_stat_result_effective_taken_at_trigger
    AFTER INSERT OR UPDATE OR DELETE ON index_stat_result
    FOR EACH ROW
    EXECUTE FUNCTION index_stat_result_effective_taken_at_trigger();

-- Backfill existing rows using the same computation the triggers use.
UPDATE files
SET effective_taken_at = COALESCE(
    (SELECT e.taken_at FROM index_exif_result e WHERE e.file_id = files.id),
    (SELECT s.last_modified AT TIME ZONE 'UTC' FROM index_stat_result s WHERE s.file_id = files.id),
    'epoch'::timestamp
);

-- Postgres cannot add columns to an existing view, so it is dropped and
-- rebuilt. effective_taken_at is added alongside the real EXIF taken_at so
-- sorting and metadata exposure stay independent.
DROP VIEW IF EXISTS file_infos;
CREATE VIEW file_infos AS
SELECT f.id,
       f.key,
       f.created_at,
       s.content_type,
       s.size_bytes,
       s.last_modified,
       e.taken_at,
       f.effective_taken_at,
       p.preview_key,
       p.width  AS preview_width,
       p.height AS preview_height
FROM files f
LEFT JOIN index_stat_result s ON s.file_id = f.id
LEFT JOIN index_exif_result e ON e.file_id = f.id
LEFT JOIN index_preview_result p ON p.file_id = f.id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

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

DROP TRIGGER IF EXISTS index_stat_result_effective_taken_at_trigger ON index_stat_result;
DROP TRIGGER IF EXISTS index_exif_result_effective_taken_at_trigger ON index_exif_result;

DROP FUNCTION IF EXISTS index_stat_result_effective_taken_at_trigger();
DROP FUNCTION IF EXISTS index_exif_result_effective_taken_at_trigger();
DROP FUNCTION IF EXISTS update_file_effective_taken_at(BIGINT);

DROP INDEX IF EXISTS files_effective_taken_at_idx;

ALTER TABLE files DROP COLUMN effective_taken_at;

-- +goose StatementEnd
