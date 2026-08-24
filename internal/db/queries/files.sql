-- name: UpsertFiles :execrows
-- Add new files and update marked_at if the listing shows a newer mtime.
INSERT INTO files (key, marked_at)
SELECT input.key, MAX(input.marked_at)
FROM (
    SELECT unnest(sqlc.arg(keys)::text[]) AS key,
           unnest(sqlc.arg(marked_ats)::timestamptz[]) AS marked_at
) input
GROUP BY input.key
ON CONFLICT (key) DO UPDATE
    SET marked_at = GREATEST(files.marked_at, EXCLUDED.marked_at);

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

-- name: ListChildPrefixes :many
-- Loose index scan over the plain btree on key (now COLLATE "C" — see
-- migrations/004_key_collation.sql) that returns up to dir_limit immediate
-- child directory names under prefix, starting strictly after `after`.
--
-- Each recursive step is one index lookup for the next key. When that key
-- belongs to a subdirectory, the *next* step seeks directly past that whole
-- subdirectory's key range in one lookup — '/' (0x2F) is immediately
-- followed by '0' (0x30) in byte order, so prefix || child || '0' sorts
-- strictly after every key under prefix || child || '/'. This is only
-- correct under byte-order (COLLATE "C") key comparison. Plain files
-- directly under prefix (no further '/') do not benefit from the skip —
-- each still costs one lookup — so a directory containing many top-level
-- files interleaved alphabetically with few subdirectories is the pathological
-- case; scan_limit bounds the damage (see below) rather than claiming this
-- is O(dir_limit) in all cases.
--
-- scan_limit bounds total recursive steps (files examined, not just
-- directories found) independent of dir_limit: WITH RECURSIVE result sets
-- are always fully materialized in Postgres, so without an explicit bound
-- the recursive term would walk every remaining key under prefix before an
-- outer LIMIT could stop it. Pass a scan_limit well above dir_limit (the
-- caller controls the multiplier); hitting it before finding dir_limit
-- directories means "no more found within budget", which the caller must
-- not conflate with "no more directories" — see server-side handling.
--
-- prefix must be the bare directory prefix (root: ""); prefix_pattern is
-- prefix || '%' (escaped by the caller, like key_pattern elsewhere). Keys
-- ending in '/' (zero-byte marker objects some tools leave behind) are
-- excluded so they can't produce an empty-named child.
--
-- The recursive CTE's own column is named `k`, not `key`: sqlc's analyzer
-- (unlike a real Postgres backend, which accepts `key` here) resolves it as
-- ambiguous against files.key once a correlated subquery references the CTE
-- inside its own recursive term. Fully qualifying every reference sidesteps
-- the analyzer limitation without changing the query's behavior.
WITH RECURSIVE walk(k, n) AS (
    (
        SELECT ff.key AS k, 1
        FROM files ff
        WHERE ff.key > sqlc.arg(after)::text
          AND ff.key LIKE sqlc.arg(prefix_pattern)
          AND ff.key NOT LIKE '%/'
        ORDER BY ff.key
        LIMIT 1
    )
    UNION ALL
    SELECT (
        SELECT ff.key
        FROM files ff
        WHERE ff.key > CASE
                WHEN strpos(substr(walk.k, char_length(sqlc.arg(prefix)::text) + 1), '/') > 0
                    THEN sqlc.arg(prefix)::text
                        || split_part(substr(walk.k, char_length(sqlc.arg(prefix)::text) + 1), '/', 1)
                        || '0'
                ELSE walk.k
            END
          AND ff.key LIKE sqlc.arg(prefix_pattern)
          AND ff.key NOT LIKE '%/'
        ORDER BY ff.key
        LIMIT 1
    ), walk.n + 1
    FROM walk
    WHERE walk.k IS NOT NULL
      AND walk.n < sqlc.arg(scan_limit)::int
)
SELECT DISTINCT
    (sqlc.arg(prefix)::text
        || split_part(substr(walk.k, char_length(sqlc.arg(prefix)::text) + 1), '/', 1)
        || '/')::text AS child
FROM walk
WHERE walk.k IS NOT NULL
  AND strpos(substr(walk.k, char_length(sqlc.arg(prefix)::text) + 1), '/') > 0
ORDER BY child
LIMIT sqlc.arg(dir_limit);
