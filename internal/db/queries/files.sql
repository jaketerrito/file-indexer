-- name: CreateFile :one
INSERT INTO files (key, content_type, size_bytes, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
