-- name: UpsertFiles :execrows
-- Crawler ingest: register discovered objects by key + listing last-modified
-- (the staleness mark). Idempotent: ON CONFLICT bumps marked_at only when
-- the listing shows a strictly newer mtime, so re-crawls of unchanged
-- objects are silent no-ops at the DB level. The stat seed step detects
-- stale done rows by comparing index_stat.mark against files.marked_at.
INSERT INTO files (key, marked_at)
SELECT unnest(sqlc.arg(keys)::text[]),
       unnest(sqlc.arg(marked_ats)::timestamptz[])
ON CONFLICT (key) DO UPDATE
    SET marked_at = GREATEST(files.marked_at, EXCLUDED.marked_at);

-- name: GetFile :one
SELECT * FROM file_infos
WHERE id = $1;

-- name: GetFilesByIDs :many
SELECT * FROM file_infos
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
-- arguments are ignored. Metadata columns come from the stat index via the
-- file_infos view and are NULL until a file is indexed; sorts fall back via
-- COALESCE so unindexed files group together instead of disappearing.

-- name: ListFilesByKeyAsc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (key, id) > (sqlc.arg(last_key)::text, sqlc.arg(last_id)::bigint))
ORDER BY key ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByKeyDesc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (key, id) < (sqlc.arg(last_key)::text, sqlc.arg(last_id)::bigint))
ORDER BY key DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByLastModifiedAsc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(last_modified, 'epoch'::timestamptz), id) > (sqlc.arg(cursor_last_modified)::timestamptz, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(last_modified, 'epoch'::timestamptz) ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByLastModifiedDesc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(last_modified, 'epoch'::timestamptz), id) < (sqlc.arg(cursor_last_modified)::timestamptz, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(last_modified, 'epoch'::timestamptz) DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesBySizeAsc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(size_bytes, 0), id) > (sqlc.arg(last_size)::bigint, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(size_bytes, 0) ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesBySizeDesc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(size_bytes, 0), id) < (sqlc.arg(last_size)::bigint, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(size_bytes, 0) DESC, id DESC
LIMIT sqlc.arg(page_limit);
