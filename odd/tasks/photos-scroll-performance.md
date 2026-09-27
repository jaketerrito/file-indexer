# Photos page scroll performance

Endless scrolling on `/photos` feels capped or slow. Investigation of the
DOKS `file-indexer` namespace showed no errors, but:

- Backend `ListFiles` sorted by `taken_at` does a full join + top-N heapsort
  over all image rows each page because the sort key
  `COALESCE(taken_at, last_modified AT TIME ZONE 'UTC', epoch)` spans two
  tables and cannot be indexed on the existing view.
- Frontend `useInfiniteQuery` uses TanStack Query defaults
  (`staleTime: 0`, `refetchOnWindowFocus: true`), so every already-loaded
  page is re-fetched whenever the window regains focus.

## Tasks

1. **Frontend cache tuning**
   - Add `staleTime` and disable `refetchOnWindowFocus` for the three
     infinite-query consumers: `PhotoWall`, `FileList`, `DirectoryList`.
   - Run web typecheck and unit tests.

2. **Backend denormalized sort column**
   - Add `files.effective_taken_at` (timestamp, not null, default epoch).
   - Create index `(effective_taken_at, id)`.
   - Maintain the column via triggers on `index_exif_result` and
     `index_stat_result`.
   - Expose `effective_taken_at` in the `file_infos` view.
   - Rewrite `ListFilesByTakenAtAsc` / `ListFilesByTakenAtDesc` to sort and
     keyset-page on `effective_taken_at`.
   - Update cursor helpers to use `EffectiveTakenAt`.
   - Regenerate sqlc and update integration tests.
   - Run Go unit + integration tests.

3. **Verify**
   - Re-measure the `ListFiles` query plan to confirm an index scan replaces
     the full sort.
   - Check the DOKS web logs after redeploy to confirm fewer redundant page
     fetches.

## Evidence

- Backend pagination check: walked 200 pages/10k unique image files via
  `SearchService.ListFiles` directly without exhausting the token.
- Pre-fix query plan: full Hash Join + top-N heapsort over 21,701 image rows
  (~125 ms execution).
- Post-fix query plan: index scan on `files_effective_taken_at_idx`
  (~1.8 ms execution).
- Commits:
  - `fadd628` fix(web): avoid refetching loaded infinite-scroll pages on focus
  - `16ae660` perf(search): index photos taken_at sort with effective_taken_at column
