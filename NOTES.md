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
  punctuation/case relative to byte value); and the upcoming directory-browsing loose index scan
  needs byte order for its "skip past a subdirectory" trick to be correct ('/' 0x2F must sort
  immediately before '0' 0x30). Folded directly into 001_initial.sql rather than a later ALTER
  migration since there's no deployed data yet (fresh project) — an ALTER would additionally have
  to drop and rebuild the file_infos view (Postgres refuses ALTER COLUMN TYPE on a column a view
  depends on), which isn't worth the extra migration/view-rebuild dance for zero rows.
  files_key_pattern_idx (the text_pattern_ops index) is dropped: it exists only to make
  LIKE 'prefix%' collation-independent, and under COLLATE "C" the column's own UNIQUE btree
  already serves that.
