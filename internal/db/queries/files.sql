-- name: UpsertFiles :execrows
-- Add new files and update marked_at if the listing shows a newer mtime.
-- seen_at is stamped to now() on every re-upsert (the column DEFAULT covers
-- the insert case) regardless of whether marked_at changed: it is a
-- liveness mark for DeleteUnseenFiles below, not a change mark, so an
-- unmodified object must still get it refreshed on every crawl that saw it.
INSERT INTO files (key, marked_at)
SELECT input.key, MAX(input.marked_at)
FROM (
    SELECT unnest(sqlc.arg(keys)::text[]) AS key,
           unnest(sqlc.arg(marked_ats)::timestamptz[]) AS marked_at
) input
GROUP BY input.key
ON CONFLICT (key) DO UPDATE
    SET marked_at = GREATEST(files.marked_at, EXCLUDED.marked_at),
        seen_at = now();

-- name: DatabaseNow :one
-- The database's clock, read by the crawler before it starts walking so the
-- sweep cutoff it later passes to DeleteUnseenFiles comes from the same
-- clock UpsertFiles stamps seen_at with (see that query's doc comment and
-- DeleteUnseenFiles below) — using the crawler process's own clock instead
-- would make the sweep's correctness depend on API/crawler clock
-- synchronization, which is not otherwise a requirement anywhere in this
-- codebase.
SELECT now()::timestamptz;

-- name: DeleteUnseenFiles :execrows
-- Deletes every files row not (re-)seen since cutoff: the reconciliation
-- half of out-of-band S3 delete handling (crawler emits nothing for an
-- object it no longer lists, so the object's row simply never gets
-- re-stamped). Race-free as long as cutoff was read (via DatabaseNow) before
-- the crawl's walk started: any row this deletes was last stamped strictly
-- before that walk began, so if the object were still in S3 the walk's own
-- listing (S3 listings are strongly consistent) would have re-stamped it
-- via UpsertFiles. An object landing mid-walk (e.g. a concurrent
-- CommitUpload) stamps seen_at after cutoff and survives. Called by
-- DeleteUnseenFilesWithDirectories (store.go), never directly — see that
-- method for why directory pruning cannot be folded into this single
-- statement.
DELETE FROM files
WHERE seen_at < sqlc.arg(cutoff)::timestamptz;

-- name: GetFile :one
SELECT * FROM file_infos
WHERE id = $1;

-- name: GetFileByKey :one
SELECT * FROM file_infos
WHERE key = $1;

-- name: GetFilesByIDs :many
SELECT * FROM file_infos
WHERE id = ANY($1::bigint[]);

-- name: DeleteFile :one
DELETE FROM files
WHERE id = $1
RETURNING *;

-- name: DeleteFilesByIDs :execrows
DELETE FROM files
WHERE id = ANY(sqlc.arg(ids)::bigint[]);

-- name: GetDirectoryStats :one
-- key_pattern is prefix || '%' (escaped by the caller). Used to populate a
-- delete-folder confirmation dialog before DeleteDirectory runs.
SELECT count(*)::bigint AS file_count,
       COALESCE(sum(size_bytes), 0)::bigint AS total_bytes
FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern);

-- name: ListFilesForDelete :many
-- Keyset-paginated (on id only — order doesn't matter for deletion, just
-- completeness and no duplicates/gaps) listing of a subtree's files, for
-- DeleteDirectory to batch through. preview_key is included so the caller
-- can batch-delete derived preview objects alongside the source objects.
SELECT id, key, preview_key FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (NOT sqlc.arg(has_cursor)::bool OR id > sqlc.arg(last_id)::bigint)
ORDER BY id ASC
LIMIT sqlc.arg(page_limit);

-- The ListFilesBy* queries below implement keyset pagination for the search
-- service: one query per (sort field, direction), always tie-breaking on id
-- so cursors are stable. key_pattern and content_type_pattern are LIKE
-- patterns built (and escaped) by the caller; an empty content_type_pattern
-- disables the content type filter. When has_cursor is false the last_*
-- arguments are ignored. Metadata columns come from the stat index via the
-- file_infos view and are NULL until a file is indexed; sorts fall back via
-- COALESCE so unindexed files group together instead of disappearing.
--
-- direct_only/dir_prefix implement browse mode's "files directly in this
-- directory" filter (SearchService.ListDirectory's files phase): when
-- direct_only is true, rows whose key has another '/' after dir_prefix (i.e.
-- lives in a deeper subdirectory) are excluded. dir_prefix must be the bare
-- prefix (no trailing '%'); when direct_only is false it is ignored. This
-- reuses the exact sort/filter/keyset logic search already has instead of a
-- parallel set of directory-scoped queries.

-- name: ListFilesByKeyAsc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(direct_only)::bool OR strpos(substr(key, char_length(sqlc.arg(dir_prefix)::text) + 1), '/') = 0)
  AND (NOT sqlc.arg(has_cursor)::bool OR (key, id) > (sqlc.arg(last_key)::text, sqlc.arg(last_id)::bigint))
ORDER BY key ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByKeyDesc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(direct_only)::bool OR strpos(substr(key, char_length(sqlc.arg(dir_prefix)::text) + 1), '/') = 0)
  AND (NOT sqlc.arg(has_cursor)::bool OR (key, id) < (sqlc.arg(last_key)::text, sqlc.arg(last_id)::bigint))
ORDER BY key DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByLastModifiedAsc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(direct_only)::bool OR strpos(substr(key, char_length(sqlc.arg(dir_prefix)::text) + 1), '/') = 0)
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(last_modified, 'epoch'::timestamptz), id) > (sqlc.arg(cursor_last_modified)::timestamptz, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(last_modified, 'epoch'::timestamptz) ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesByLastModifiedDesc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(direct_only)::bool OR strpos(substr(key, char_length(sqlc.arg(dir_prefix)::text) + 1), '/') = 0)
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(last_modified, 'epoch'::timestamptz), id) < (sqlc.arg(cursor_last_modified)::timestamptz, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(last_modified, 'epoch'::timestamptz) DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesBySizeAsc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(direct_only)::bool OR strpos(substr(key, char_length(sqlc.arg(dir_prefix)::text) + 1), '/') = 0)
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(size_bytes, 0), id) > (sqlc.arg(last_size)::bigint, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(size_bytes, 0) ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListFilesBySizeDesc :many
SELECT * FROM file_infos
WHERE key LIKE sqlc.arg(key_pattern)
  AND (sqlc.arg(content_type_pattern)::text = '' OR content_type LIKE sqlc.arg(content_type_pattern))
  AND (NOT sqlc.arg(direct_only)::bool OR strpos(substr(key, char_length(sqlc.arg(dir_prefix)::text) + 1), '/') = 0)
  AND (NOT sqlc.arg(has_cursor)::bool OR (COALESCE(size_bytes, 0), id) < (sqlc.arg(last_size)::bigint, sqlc.arg(last_id)::bigint))
ORDER BY COALESCE(size_bytes, 0) DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListContentTypeCategories :many
-- Distinct top-level MIME categories in the stat index, each with a trailing
-- "/" so values plug straight into the content_type_pattern prefix semantics
-- of the ListFilesBy* queries above. Sourced from index_stat_result (columns
-- NOT NULL there), not the file_infos view: files not yet stat-indexed have
-- no content type and contribute nothing. The strpos guard drops a malformed
-- type with no "/", whose derived category would match nothing.
SELECT DISTINCT split_part(content_type, '/', 1) || '/' AS category
FROM index_stat_result
WHERE strpos(content_type, '/') > 0
ORDER BY category;
