-- Result storage for the stat index type. Written inside the same
-- transaction as CompleteIndexQueue('stat', ...).

-- name: UpsertIndexStatResult :exec
INSERT INTO index_stat_result (file_id, content_type, size_bytes, last_modified)
VALUES (sqlc.arg(file_id), sqlc.arg(content_type), sqlc.arg(size_bytes), sqlc.arg(last_modified)::timestamptz)
ON CONFLICT (file_id) DO UPDATE
    SET content_type  = EXCLUDED.content_type,
        size_bytes    = EXCLUDED.size_bytes,
        last_modified = EXCLUDED.last_modified,
        updated_at    = now();
