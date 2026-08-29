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
-- name: ListIndexPreviewKeys :many
-- Every preview object key the index claims ownership of. The preview GC
-- set-diffs a bucket listing of the previews/ prefix against this: a listed
-- key absent here is an orphan (its row vanished by files-row cascade or its
-- PUT never reached Complete) and is deleted from storage.
SELECT preview_key FROM index_preview_result;
