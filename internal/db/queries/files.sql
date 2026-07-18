-- name: UpsertFile :one
INSERT INTO files (key, content_type, size_bytes, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (key) DO UPDATE SET
    content_type = EXCLUDED.content_type,
    size_bytes   = EXCLUDED.size_bytes,
    updated_at   = EXCLUDED.updated_at
RETURNING *;

-- name: GetFile :one
SELECT * FROM files
WHERE id = $1;

-- name: GetFilesByIDs :many
SELECT * FROM files
WHERE id = ANY($1::bigint[]);

-- name: DeleteFile :one
DELETE FROM files
WHERE id = $1
RETURNING *;

-- The ListFilesBy* queries below implement keyset pagination for the search
-- service: one query per (sort field, direction), always tie-breaking on id
-- so cursors are stable. key_pattern and content_type_pattern are LIKE
-- patterns built (and escaped) by the caller; an empty content_type_pattern
-- disables the content type filter. When has_cursor is false the last_*
-- arguments are ignored.

-- name: ListFilesByKeyAsc :many
SELECT * FROM files
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (key, id) > (sqlc.arg(last_key)::text, sqlc.arg(last_id)::bigint))
ORDER BY key ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByKeyDesc :many
SELECT * FROM files
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (key, id) < (sqlc.arg(last_key)::text, sqlc.arg(last_id)::bigint))
ORDER BY key DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByCreatedAtAsc :many
SELECT * FROM files
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (created_at, id) > (sqlc.arg(last_created_at)::timestamptz, sqlc.arg(last_id)::bigint))
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByCreatedAtDesc :many
SELECT * FROM files
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (created_at, id) < (sqlc.arg(last_created_at)::timestamptz, sqlc.arg(last_id)::bigint))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesBySizeAsc :many
SELECT * FROM files
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(size_bytes, 0), id) > (sqlc.arg(last_size)::bigint, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(size_bytes, 0) ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesBySizeDesc :many
SELECT * FROM files
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(size_bytes, 0), id) < (sqlc.arg(last_size)::bigint, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(size_bytes, 0) DESC, id DESC
LIMIT sqlc.arg(page_limit);
