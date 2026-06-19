-- name: CreateFile :one
INSERT INTO files (key, content_type, size_bytes, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
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
