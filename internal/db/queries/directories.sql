-- Queries for the directories table (see migrations/001_initial.sql for the
-- schema and the rationale for existence-only, application-maintained
-- derivation). Two shapes repeat throughout this file:
--
-- Ancestor explosion: given a set of keys, compute every distinct proper
-- ancestor directory. "docs/sub/a.txt" contributes "docs/" and "docs/sub/";
-- a bare "top.txt" contributes nothing (the root is never stored). Marker
-- keys ending in "/" are excluded so a zero-byte object some external tool
-- left behind at exactly a directory's own key can't produce a bogus
-- empty-named child one level down.
--
-- Subtree-emptiness test: whether any file's key falls strictly inside a
-- candidate directory. Implemented as a byte range
-- (key > path AND key < skip-past(path)), not `key LIKE path || '%'`,
-- because a per-row non-constant LIKE pattern cannot drive an index range
-- scan the way a two-sided inequality can — confirmed via EXPLAIN to
-- produce a Nested Loop [Anti] Join with an Index [Only] Scan on files'
-- key btree, not a sequential scan, at both small and larger (2000+
-- directory) candidate counts. `left(path, -1) || '0'` reuses the same
-- byte-order fact ListDirectory's cursor pagination does ('/' 0x2F sorts
-- immediately before '0' 0x30): it is the smallest key that sorts after
-- every key in that directory's subtree. Only correct under files.key's
-- COLLATE "C".

-- name: UpsertDirectoriesForKeys :exec
-- Inserts every ancestor directory implied by keys that doesn't already
-- exist. Called after a batch of files is written (see store.go); ON
-- CONFLICT DO NOTHING makes it idempotent against directories that already
-- exist from a sibling file.
INSERT INTO directories (path)
SELECT DISTINCT array_to_string(parts[1:i], '/') || '/'
FROM (
    SELECT string_to_array(k, '/') AS parts
    FROM unnest(sqlc.arg(keys)::text[]) AS k
    WHERE k NOT LIKE '%/'
) x,
generate_subscripts(x.parts, 1) AS i
WHERE i < array_length(x.parts, 1)
ON CONFLICT (path) DO NOTHING;

-- name: PruneDirectoriesForKeys :exec
-- Deletes every ancestor directory implied by keys that no longer has any
-- file under it. Called after a batch of files is deleted (see store.go),
-- in the same transaction and after the delete, so the subtree-emptiness
-- test below observes post-delete state. Scoped to keys' own ancestors
-- (rather than PruneOrphanDirectories' unscoped scan) so a single-file or
-- single-batch delete stays cheap.
DELETE FROM directories d
WHERE d.path IN (
    SELECT DISTINCT array_to_string(parts[1:i], '/') || '/'
    FROM (
        SELECT string_to_array(k, '/') AS parts
        FROM unnest(sqlc.arg(keys)::text[]) AS k
        WHERE k NOT LIKE '%/'
    ) x,
    generate_subscripts(x.parts, 1) AS i
    WHERE i < array_length(x.parts, 1)
)
AND NOT EXISTS (
    SELECT 1 FROM files f
    WHERE f.key > d.path
      AND f.key < left(d.path, -1) || '0'
      AND f.key NOT LIKE '%/'
);

-- name: PruneOrphanDirectories :exec
-- Deletes every directory row with no file under it: the unscoped
-- counterpart to PruneDirectoriesForKeys (which only checks a given
-- batch's own ancestors). Load-bearing for DeleteUnseenFilesWithDirectories
-- (internal/db/store.go), which backs the crawler's out-of-band S3 delete
-- reconciliation: DeleteUnseenFiles could instead return the swept keys and
-- feed the scoped PruneDirectoriesForKeys (there is no fundamental
-- obstacle — RETURNING a key list works fine), but an out-of-band sweep has
-- no natural bound on victim count (wrong bucket, or a large prefix deleted
-- directly in S3, could be most of the table), and streaming that many keys
-- through the crawler process is worse than one unscoped pass over
-- directories — a table sized by directory count, not file count (see the
-- 2000+-candidate EXPLAIN measurement above). Beyond that use, this is also
-- cheap insurance against drift from any other cause (manual SQL, a future
-- write path that skips Store) — every insert/delete of files otherwise
-- runs through Store's WithDirectories methods, which keep directories in
-- sync inline in the same transaction, so no other write path is expected
-- to ever produce an orphan.
DELETE FROM directories d
WHERE NOT EXISTS (
    SELECT 1 FROM files f
    WHERE f.key > d.path
      AND f.key < left(d.path, -1) || '0'
      AND f.key NOT LIKE '%/'
);

-- name: ListChildDirectories :many
-- Immediate children of parent ("" for root), keyset-paged on path. Index-
-- only scan on directories_parent_path_idx: exact, O(page_limit), no scan
-- budget, no truncation — replaces the old ListChildPrefixes loose index
-- scan over files entirely.
SELECT path FROM directories
WHERE parent = sqlc.arg(parent)
  AND (NOT sqlc.arg(has_cursor)::bool OR path > sqlc.arg(after)::text)
ORDER BY path
LIMIT sqlc.arg(page_limit);
