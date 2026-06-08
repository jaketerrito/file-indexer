-- name: CreateFile :one
INSERT INTO files (source, path, content_type, size_bytes)
VALUES ($1, $2, $3, $4)
RETURNING *;
