# UI Redesign — Issue #139

Scope: frontend-only redesign of the web app. No backend changes.

## Status

All implementation tasks complete. Verified with typecheck, lint, unit tests, and the live Tilt webdev environment.

## Changes made

1. **Global layout: pinned top navigation + search bar**
   - `web/src/routes/__root.tsx` has a sticky header with Files / Recents / Photos links on the left and the search bar next to them.
   - No sidebar, no "file-indexer" home button, no page titles.

2. **Recents page**
   - Added `web/src/routes/recents.tsx`.
   - Reuses the shared file table, hides query/type/sort controls, defaults to `lastModified` descending.
   - No folders.

3. **Photos page**
   - Added `web/src/routes/photos.tsx`.
   - Same as Recents but filtered to `image/`.

4. **Shared file table redesign**
   - `web/src/components/FileTable.tsx`:
     - Removed the Type column.
     - Generic content-type emoji icons for non-image files (`web/src/lib/fileIcon.ts`).
     - Human-readable dates via `web/src/lib/formatDate.ts`.
     - Open / Download / Move / Delete moved into a triple-dot menu per row.
     - **Open was removed from the row menu**; the file page now has the Open action.
     - Folder rows also have a triple-dot menu with **Delete folder**.
     - Row click opens the file page.

5. **Files browse toolbar**
   - `web/src/components/Browser.tsx`: removed the Sort by bar; New folder and Upload buttons sit on the right side of the breadcrumb/toolbar row.

6. **Breadcrumbs**
   - `web/src/components/Breadcrumbs.tsx`: Home is always clickable, and the last segment can be made clickable via `lastIsCurrent={false}`.

7. **File page layout**
   - `web/src/routes/file.$id.tsx`: actions and metadata in a right-hand side panel.
   - Parent directory path uses the same `Breadcrumbs` component as the Files list, with the parent directory itself clickable.
   - In-page file preview deferred.

8. **Tests**
   - Updated `FileList.test.tsx`, `DirectoryList.test.tsx`, `Breadcrumbs.test.tsx`.
   - Added/updated `recents.test.tsx`, `photos.test.tsx`, `__root.test.tsx`, `index.test.tsx`, `search.test.tsx`.

9. **Verification**
   - `npm --prefix web run typecheck` ✅
   - `npm --prefix web run lint` ✅
   - `npm --prefix web run test` ✅ (240 tests)
   - Live Tilt webdev environment: `http://web.ui-overhaul-v2.localhost` ✅

## Open question

- In-page file preview on the file page: deferred. It would need MIME-aware rendering or a signed inline URL; the current side-panel layout is ready for it.
