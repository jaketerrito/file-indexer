5/10/26
- will most likely need to make the crawler use worker pool for waiting on indexer results

6/1/26
- indexer write stuff to database (repository pattern for writing to db)

6/12/26
- indexer turn the byte stream into reader instance so i can pass to exif package
- indexing is not idempotent: CreateFile is a plain INSERT, so re-crawling fails on UNIQUE(source, path). Need ON CONFLICT (source, path) DO UPDATE upsert; checksum column would let us skip unchanged files
- decision: reference-based indexing. crawler/api pass only (source, s3 key) and the indexer pulls bytes from s3 and computes all metadata. reasoning:
  - remote sources pushing bytes for indexing only breaks source of truth (web app couldn't serve those files) -> remote sources become uploaders, everything lands in s3 first
  - teeing upload bytes to the indexer saves at most one GET but adds failure modes (phantom index entries, upload coupled to indexer availability); pull path is required anyway for re-indexing and crawler-discovered files
  - no inline metadata compute in the api — keep ingest dumb, indexer owns all metadata

6/14/26
- logs in crawler are nonsense. need start, stop, proper reference on that
- crawler should be updated to properly check if each file is actually in need of reindexing. (indexer shouldnt do that, should treat index requests as commnad to refresh the indexed data)

6/15/26
- use single file info across the board from storage.stat -> db schema -> db create file args
- logs in indexer for each file processsed

6/16/26
- defer uploads, will handle that later in favor of just using the crawler

7/2/26
- setup grpc swagger type thing for files and indexer? Or portforward with reflection then use an external tool

7/3/26
- search service: integration tests currently cover the db list queries only; add full-service integration tests that exercise SearchService.ListFiles over gRPC against the deployed service (cursor paging, filters, sorting)
- consider protovalidate (buf) + a shared server interceptor for declarative request validation (e.g. page_size bounds, enum defined_only) across all services; defaulting logic (unspecified sort -> key asc, page_size 0 -> 50) stays in code
- trivy image scan job currently rebuilds images for indexer + migrate from scratch. Should instead scan images already built by test.yml's `tilt ci` — either by exporting as artifacts across jobs (same workflow) or pushing to a registry first.
- tilt ci should only be relied on for integration tests (set it up to only run minimum necessary changes for that)

7/5/26
- The crawler process needs to clean up files in the db that don't exist in s3. Should also do something instead of error when it finds file that already exists in db... perhaps upsert?
- e2e/integration tests with playwright against the tilt environment (slot into the existing test-integration flow); unit tests only for now
- styling/design system — UI is intentionally bare semantic HTML
- content-type filter dropdown is hardcoded (image/, video/, ...); should be populated from the backend, e.g. a SearchService RPC returning distinct content-type categories (TODO in FileList.tsx)
- SSR data fetching: the file list fetches client-side after hydration. consider router loader + react-query SSR integration so the first page renders server-side
- tilt web dev loop does a full image rebuild per change (no live_update); iterate with `npm run dev` against the 50052/50053 port-forwards instead. consider live_update or a tilt-managed dev server later

7/26/26
- preview index writes derived images to the same bucket under INDEX_PREFIX (default .index/); crawler skips that prefix so derived objects never become files rows
- orphaned preview blobs need a GC job: DeleteFile removes previews best-effort only, files deleted from s3 out of band never trigger it, and index_preview_result rows vanish by cascade without touching storage. Job should list INDEX_PREFIX + "previews/" and delete keys with no matching index_preview_result row
- preview URLs are presigned per request, so the browser HTTP cache never hits across page loads. If thumbnail bandwidth becomes a problem, swap GetPreviewURL for a cacheable BFF route serving bytes with immutable cache headers
- no HEIC/AVIF previews: no pure-Go decoder and the binaries are CGO_ENABLED=0 on scratch

7/27/26
- exif index type added (github.com/evanoberholster/imagemeta, pure Go, CGO_ENABLED=0-safe):
  extracts EXIF + XMP into index_exif_result. Not joined into file_infos (still not part of the
  search/sort read model — revisit once there's a concrete need, e.g. sort by taken_at), but now
  exposed read-only via FilesService.GetFileInfo's optional `exif` field for the frontend
  metadata modal (proto/service/v1/files.proto ExifMetadata; internal/service/files/server.go
  GetFileInfo). Absent index_exif_result row (not yet indexed, or neither EXIF nor XMP present)
  maps to an unset `exif` field, same absent-row convention as everywhere else this table is read.
- taken_at is TIMESTAMP not TIMESTAMPTZ on purpose: EXIF DateTimeOriginal carries no time zone,
  so attaching one (UTC or otherwise) would fabricate an offset the source never specified.
  gps_at and xmp_create_date are the opposite — TIMESTAMPTZ — since GPSTimeStamp is spec'd UTC
  and XMP xmp:CreateDate genuinely carries a parsed offset; verified both empirically against
  the library before picking column types, don't assume "it's a timestamp near EXIF" implies
  naive
- GPS lat/lon/alt stored at full precision (single-tenant bucket for now) and now exposed as-is
  through GetFileInfo's ExifMetadata (metadata modal, 2026-08); if this ever goes multi-tenant or
  gets exposed externally, revisit — both the DB precision and the API exposure decision were
  made assuming a private, single-tenant bucket
- known imagemeta MakerNote issue: some Sony ARW lens fields come back garbled (upstream
  reverse-offset bug) — sanitize()/maxSanitizedFieldLen bounds the damage but doesn't fix it
- candidate gate is content-type `image/` OR a fixed RAW extension allowlist: S3/MinIO reports
  camera RAW objects as application/octet-stream (no registered MIME type), so a content-type-only
  gate (as index-preview uses) would silently skip every RAW file
- two-tier bounded read (header, then full-file fallback only if the header read was truncated
  and found nothing): measured every modern format's metadata resolves within ~200KB and forward-only,
  Canon's legacy CRW format is the only one needing the fallback (its directory is at EOF)
- no video/audio/PDF metadata yet — imagemeta covers images + camera RAW only; a future
  mp4/id3 extractor could reuse index_exif_result's common columns (make, model, taken_at, gps,
  dimensions) rather than inventing a parallel schema

8/16/26
- upload finally implemented (deferred 6/16), presigned-PUT approach: FilesService.GetUploadURL
  returns a presigned PUT URL (browser writes bytes straight to S3, no bytes transit the API);
  FilesService.CommitUpload then stats the key itself (never trusts the client) and upserts a
  files row via the same UpsertFiles path the crawler uses, so the next index-queue seed picks it
  up. No new Storage method for content-type binding: minio-go's PresignedPutObject doesn't sign
  headers, so the client's Content-Type header on the PUT isn't verified — acceptable since
  CommitUpload's Stat reads back whatever S3 actually stored rather than trusting the request.
- key collisions overwrite (idempotent upsert), which also happens to give free re-indexing (bumped
  marked_at re-seeds the queue) if a client re-uploads an existing key.
- frontend: FileList's Upload button uploads multiple selected files sequentially (not
  concurrently) so one failure doesn't leave a pile of in-flight requests; each file does
  presign -> PUT -> commit in turn.
- considered a full folder-browsing UI (breadcrumb nav, separate from search) since we do expect
  hierarchical dirs (docs/important, docs/housing, ...); deferred — no delimiter-based "list
  immediate children" query exists yet (current ListFilesBy* are recursive LIKE 'prefix%', not
  S3-Delimiter-style), so folder rows would need a new backend query. Landed the cheap version
  instead: an "Upload to" text input (web/src/lib/uploadPath.ts normalizes it — trailing slash,
  collapsed//, trimmed) decoupled from the search prefix, so typing a search term can never change
  where an upload lands. Revisit real folder browsing if the flat-search UX proves annoying.

8/23/26
- files.key is now COLLATE "C" (raw byte order) rather than the database's default collation, for
  two independent reasons that turn out to have the same fix: key sort order now agrees with what
  S3's ListObjectsV2 actually returns (it never did before — locale-aware collations reorder
  punctuation/case relative to byte value); and the directories table's subtree-emptiness check
  (see below) needs byte order for a range-bound trick to be correct ('/' 0x2F must sort
  immediately before '0' 0x30). Folded directly into 001_initial.sql rather than a later ALTER
  migration since there's no deployed data yet (fresh project) — an ALTER would additionally have
  to drop and rebuild the file_infos view (Postgres refuses ALTER COLUMN TYPE on a column a view
  depends on), which isn't worth the extra migration/view-rebuild dance for zero rows.
  files_key_pattern_idx (the text_pattern_ops index) is dropped: it exists only to make
  LIKE 'prefix%' collation-independent, and under COLLATE "C" the column's own UNIQUE btree
  already serves that.
- recursive directory delete (FilesService.GetDirectoryStats + DeleteDirectory) is deliberately
  not atomic, and the proto doc comment says so: it pages through the subtree
  (ListFilesForDelete, keyset on id), and only deletes a batch's DB rows after that batch's S3
  objects (source + preview) are confirmed gone (new Storage.DeleteMany). A batch's S3 failure
  stops the whole call without touching that batch's DB rows, so a retry with the same path picks
  up where it left off — S3 no-ops keys already deleted, and the DB rows for a half-deleted batch
  are still there to retry. The alternative (delete DB rows regardless of which individual S3
  deletes within a batch succeeded) was rejected: Storage.DeleteMany returns one combined error,
  not per-key results, so there's no way to know which subset actually succeeded.
  GetDirectoryStats exists purely to back a client-side delete confirmation dialog (file count +
  total bytes) — DeleteDirectory itself has no dry-run flag.
- fixed in passing while adding this: FilesServer.validateUploadKey's ".." check was
  strings.Contains(key, ".."), which rejected legitimate filenames like "archive..2026.zip" that
  merely contain two consecutive dots without meaning "parent directory". Both it and the new
  validateDirPath now check for ".." as a whole path segment (split on "/", compare each piece)
  via a shared hasDotDotSegment helper.
- directory browsing (SearchService.ListDirectory) is backed by a real, materialized directories
  table (path TEXT PRIMARY KEY + a GENERATED parent column), not derived at read time. Originally
  built as a loose-index-scan recursive CTE over files (ListChildPrefixes) instead — abandoned
  after realizing it has a real silent-data-loss bug, not just a performance edge case: its
  scan_limit budget and the query's own doc comment both admit "hitting it before finding
  dir_limit directories means 'no more found within budget', which the caller must not conflate
  with 'no more directories'" — and the server did exactly that conflation (server.go's
  DIRECTORIES-phase code treated any shortfall as exhaustion and never resumed that phase). A
  directory with enough top-level files sorting before a subdirectory made that subdirectory
  permanently invisible, with no error and no truncation flag anywhere in the API. The whole
  scan-budget concept — dirScanMultiplier, maxDirScan, skipPastChild, ListChildPrefixes' CTE — is
  gone; ListChildDirectories is now a plain keyset SELECT off a (parent, path) index: exact,
  O(page_limit), no budget to exhaust.
- directories is second-order derived, same relationship files has to S3 (rebuildable from files
  alone — every insert runs UpsertDirectoriesForKeys in the same transaction as the files write
  that produced it, so a full crawl reconstructs it as a byproduct with no dedicated rebuild query
  needed), so this does not violate "S3 is the source of truth" — it's one hop further from S3
  than files itself, not a competing source.
  Existence only, no file_count/total_bytes: sizes come from index_stat_result, written
  asynchronously by a different worker at a different time than files/directories are written, so
  folding that in would mean a second, much larger drift surface for a number GetDirectoryStats
  can already compute correctly (just less cheaply, by scanning the subtree directly).
- maintenance is application-level (internal/db/store.go's Store type), not a database trigger,
  after actually thinking through the tradeoff rather than defaulting to "trigger = safer": the
  SQL required is identical either way (UpsertDirectoriesForKeys/PruneDirectoriesForKeys vs. the
  same logic in trigger form), and a trigger's real advantage — guarding against future write
  paths nobody remembered to update — isn't worth it when there are exactly three write paths
  (UpsertFiles/DeleteFile/DeleteFilesByIDs) and all three are now hidden behind narrow interfaces
  (crawler.FileStore, files.FileIndex) that simply don't expose the un-wrapped queries anymore —
  it's a compile error to write files without also writing directories from any code this repo
  controls. Prune runs after the delete, in the same transaction, so its subtree-emptiness check
  observes post-delete state; each DeleteDirectory batch is now atomic as a side effect (its own
  cross-batch non-atomicity, described above, is unchanged).
- there is no RebuildDirectories query, on purpose (an earlier version of this had one, removed
  after review): the crawler's own per-batch UpsertFilesWithDirectories already inserts every
  directory a crawl could produce (ON CONFLICT DO NOTHING), so a separate unscoped rebuild pass
  afterward would only re-derive the same rows from the same keys via the same ancestor-explosion
  SQL — it could never find anything the per-batch insert missed, and (being the same SQL) could
  never catch a bug in that SQL either. PruneOrphanDirectories doesn't have a symmetric argument
  against it: it's also a no-op today (nothing currently creates an orphan directory row — see its
  doc comment in queries/directories.sql), but it becomes load-bearing the moment the crawler gains
  the ability to remove files rows for objects deleted from S3 out of band, since "delete whatever
  we didn't just see" is a set-difference delete with no natural per-key list to scope
  PruneDirectoriesForKeys to. Kept, called every crawl, as cheap insurance in the meantime.
- the subtree-emptiness check (used by both PruneDirectoriesForKeys and the unscoped
  PruneOrphanDirectories) is a byte range — key > path AND key < skip-past(path) — not
  key LIKE path || '%'. A per-row non-constant LIKE pattern can't drive an index range scan the
  way a two-sided inequality can; confirmed via EXPLAIN this produces a Nested Loop [Anti] Join
  with an Index [Only] Scan on files' key btree (not a sequential scan) at both small and larger
  (2000+ candidate) directory counts, executing in ~1.7ms for 2003 candidates against 5001 files.
  skip-past(path) is left(path, -1) || '0', the same '/' → '0' byte-order fact the old
  ListChildPrefixes skip trick used, just applied as a bound instead of driving a walk.
- the crawler runs PruneOrphanDirectories once at the end of every Run() — a cheap (one statement
  over the whole keyspace) safety net that does nothing today (see above for why there's no
  RebuildDirectories to pair with it) but is already in place for when crawler deletes land (see
  7/5/26 above).
- generated columns need an IMMUTABLE expression; verified substring(text, text) (the two-arg
  regex overload used for parent) is provolatile='i' on Postgres 18 before committing to the
  design, both by querying pg_proc directly and by the CREATE TABLE itself succeeding (Postgres
  rejects a non-immutable generated-column expression outright).
- property-tested rather than just example-tested: TestDirectoriesConsistencyAfterRandomMutations
  runs 200 random file creates/deletes through Store's WithDirectories wrappers, then compares the
  resulting directories rows against an expected set computed independently in Go (plain string
  splitting on the keys still believed live) rather than by re-running any of the SQL under test.
  That independence matters: an earlier version compared against a second invocation of the same
  ancestor-explosion SQL (via the since-removed RebuildDirectories), which can only catch a missed
  call, never a bug in the shared SQL itself, since both sides would compute the identical wrong
  answer. This is the test that actually catches maintenance bugs no hand-written example happens
  to exercise; it caught two while being written, both in the test's own oracle rather than the
  implementation — first comparing against the whole table instead of scoping to the test's own key
  prefix (PruneOrphanDirectories correctly prunes other tests' directory rows that were never backed
  by a real files row), then forgetting that a test's own unique prefix is itself a real ancestor
  directory (it has children), so trimming it off before computing expected ancestors under-counted
  by one.
