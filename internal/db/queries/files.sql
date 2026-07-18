-- name: UpsertFiles :many
-- Crawler ingest: register discovered objects idempotently. created_at and
-- updated_at both start as the object's S3 last-modified time. On re-crawl,
-- the row is only touched when size or last-modified actually changed, so
-- RETURNING yields exactly the new + changed file ids; the crawler feeds
-- those into ResetIndexStat to trigger re-indexing (new files have no
-- index_stat row yet and are picked up by SeedIndexStat instead).
--
-- Timestamps are truncated to whole seconds: the stat indexer also writes
-- updated_at, and its source (the HTTP Last-Modified header) only has second
-- precision while listings carry sub-second precision. Truncating on every
-- write keeps the change comparison below from false-flagging files whose
-- updated_at was last written by the indexer.
INSERT INTO files (key, size_bytes, created_at, updated_at)
SELECT t.key, t.size_bytes, date_trunc('second', t.last_modified), date_trunc('second', t.last_modified)
FROM (
    SELECT unnest(sqlc.arg(keys)::text[])                  AS key,
           unnest(sqlc.arg(sizes)::bigint[])               AS size_bytes,
           unnest(sqlc.arg(last_modifieds)::timestamptz[]) AS last_modified
) AS t
ON CONFLICT (key) DO UPDATE
SET size_bytes = EXCLUDED.size_bytes,
    updated_at = EXCLUDED.updated_at
WHERE files.size_bytes IS DISTINCT FROM EXCLUDED.size_bytes
   OR files.updated_at IS DISTINCT FROM EXCLUDED.updated_at
RETURNING id;

-- name: UpdateFileMetadata :exec
-- Stat indexer result write: metadata the S3 listing cannot provide (content
-- type) plus authoritative size/mtime from StatObject. updated_at is
-- truncated to whole seconds to match UpsertFiles (see the comment there);
-- otherwise the crawler's change detection would flag every indexed file.
UPDATE files
SET content_type = $2,
    size_bytes = $3,
    updated_at = date_trunc('second', sqlc.arg(updated_at)::timestamptz)
WHERE id = $1;

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
