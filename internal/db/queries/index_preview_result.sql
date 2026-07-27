-- Result storage for the preview index type. Written inside the same
-- transaction as CompleteIndexQueue('preview', ...).

-- name: UpsertIndexPreviewResult :exec
INSERT INTO index_preview_result (file_id, preview_key, width, height)
VALUES (sqlc.arg(file_id), sqlc.arg(preview_key), sqlc.arg(width), sqlc.arg(height))
ON CONFLICT (file_id) DO UPDATE
    SET preview_key = EXCLUDED.preview_key,
        width       = EXCLUDED.width,
        height      = EXCLUDED.height,
        updated_at  = now();
