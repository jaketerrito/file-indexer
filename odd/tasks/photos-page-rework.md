# Photos page rework

Make `/photos` a photo wall: image previews only, sorted by capture date
(`taken_at`) falling back to object last-modified, with a day separator, and
clicking a preview opens the file page.

## Backend support

- Add `SORT_FIELD_TAKEN_AT` to `proto/service/v1/search.proto`.
- Update `proto/cursor/v1/cursor.proto` with a `taken_at` sort value.
- Add a migration that joins `index_exif_result.taken_at` into `file_infos`.
- Add `ListFilesByTakenAtAsc` / `ListFilesByTakenAtDesc` queries that order by
  `COALESCE(taken_at, last_modified AT TIME ZONE 'UTC')` with keyset pagination.
- Regenerate sqlc / protobuf / mocks.
- Dispatch the new sort in `internal/service/search/server.go` and teach the
  cursor helpers about it.
- Surface `taken_at` through `FileInfo.exif` in `dbFileToProto`.
- Add unit + integration test coverage for the new sort.

## Web API mapping

- Add `'takenAt'` to `SORT_FIELDS` in `web/src/server/impl.ts` and map it to
  `SortField.TAKEN_AT`.
- Extend `FileDto` with `takenAt` and `updatedAt`, populated from `FileInfo`.
- Regenerate TS protobuf clients.
- Update impl tests and any affected fixtures.

## Frontend

- Create `PhotoWall` component (`web/src/components/PhotoWall.tsx`) that:
  - fetches image files via `listFiles` with `contentType: 'image/'`,
    `sortField: 'takenAt'`, `sortOrder: 'desc'`;
  - groups loaded files by effective date (`takenAt ?? updatedAt`);
  - renders a day heading for each group;
  - renders only the preview thumbnails in a grid;
  - clicking a preview navigates to `/file/$id`;
  - supports infinite scroll and preview-status polling.
- Rework `web/src/routes/photos.tsx` to render `PhotoWall` without filter
  controls.
- Update `web/src/routes/photos.test.tsx` and add component-level tests for
  grouping / separators.

## Verification

- `just generate`
- `npm --prefix web run typecheck`
- `go test ./...`
- `go test -tags=integration ./internal/service/search/... ./internal/db/...`
- `npm --prefix web test -- --run` (vitest unit tests)
