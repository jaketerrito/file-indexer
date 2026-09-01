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
- orphaned preview blobs need a GC job: DeleteFile removes previews best-effort only, files deleted from s3 out of band never trigger it, and index_preview_result rows vanish by cascade without touching storage. Job should list INDEX_PREFIX + "previews/" and delete keys with no matching index_preview_result row — implemented in cmd/preview-gc and internal/service/previewgc
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
- frontend: Browser.tsx is the new top-level component (index.tsx renders it instead of FileList
  directly) that switches between search mode (FileList, unchanged since 8/16) and browse mode
  (new Breadcrumbs + DirectoryList). filters.path is undefined in search mode and a string
  (possibly "") in browse mode; setting either path or prefix/type clears the other, since
  ListDirectory and ListFiles are different requests, not a shared one with optional params.
  Choosing a content-type filter while browsing switches to search mode seeded with the current
  path as a recursive prefix — folders are for navigation, filtering is search's job, so there's
  no attempt to support "show only images in this folder" as a browse-mode feature.
  "New folder" has no backend call: it just navigates to a path nothing lives under yet (matches
  the derived-directory model above — directories exist only where files do, so an empty one has
  no row until something is uploaded into it).
- 8/25/26: uploading is browse-mode-only — FileList (search mode) has no upload UI at all anymore.
  Originally FileList kept its own freeform "Upload to" text field (no path to default to in
  search mode) alongside Browser's folder-scoped upload. Simplified to a single upload path:
  navigate to the right folder, then upload there. Removes an entire redundant destination-input
  UI and its tests; search stays pure search.
- 8/25/26: both upload buttons (FileList's and Browser's) were dead in every real browser except
  the ones covered by unit tests — clicking did nothing, no request ever left the page. Root
  cause: the hidden `<input type="file">` behind each button used `display: none`. WebKit/Safari
  silently refuses to open the native file picker from a programmatic `.click()` on a
  `display: none` input (no error, just a no-op); Chrome and Firefox don't have this restriction,
  which is exactly why the existing unit tests (which fire `change` directly on the hidden input,
  never actually invoking the button's `.click()` forwarding) never caught it. Fixed by switching
  to the standard "visually hidden" a11y pattern (`lib/visuallyHidden.ts`) — clipped to nothing,
  off the visual flow, but still a real node in the render tree in every browser, so `.click()`
  keeps working everywhere. Added a regression test in both files pinning `style.display !==
  'none'`, since the failure mode has zero unit-test signal otherwise.
- TODO: no "indexing…" status in the UI. `previewUrl: null` (impl.ts listFilesImpl/listDirectory)
  currently means two different things the frontend can't tell apart: "this content type never
  gets a preview" (a .txt file) and "the preview index type hasn't processed this file yet" (a
  freshly uploaded .jpg, before the indexer's next poll claims its index_queue row). A file
  uploaded through Browser/FileList shows with no thumbnail and no visual difference from one
  that will simply never have one — the only way to tell right now is to keep refreshing and see
  if a thumbnail eventually appears. index_queue already has exactly the status this needs
  (pending/claimed/done/error per (index_type, file_id) — migrations/001_initial.sql) but nothing
  in the read path surfaces it: ListFiles/ListDirectory join file_infos for preview_key/width/height,
  never index_queue. Fix is server-side (join or a second query keyed on file_id + 'preview',
  expose a status enum alongside previewUrl) plus a frontend loading/pending affordance
  (skeleton thumbnail, subtle "Processing…" label) instead of the current bare blank. Same gap
  applies to exif/stat results shown in FileMetadataModal, though preview is the visually obvious
  one — a list of thumbnails with silent gaps reads as broken, not as "still working".

8/25/26
- crawler now reconciles files deleted from S3 out of band (resolves the 7/5/26 item above):
  mark-and-sweep on a new files.seen_at column (migrations/004_seen_at.sql), distinct from
  marked_at (object mtime, used for edit/staleness detection) — an untouched object's mtime never
  advances, so marked_at can't double as a liveness mark. UpsertFiles stamps seen_at = now() on
  every insert and every re-upsert (queries/files.sql); crawler.Run reads a cutoff via the new
  DatabaseNow query before Walk starts, then after Walk and every flush succeed, calls
  Store.DeleteUnseenFilesWithDirectories(cutoff), which deletes every files row not re-stamped
  since cutoff and prunes the directories that orphans, in one transaction.
- two other designs considered and rejected: (1) a sorted merge-join exploiting S3's listing order
  agreeing with files.key's COLLATE "C" byte order (established 8/23/26) — rejected for a
  destructive path specifically because correctness would then depend on that order equivalence
  holding for every key, whereas mark-and-sweep's correctness argument (below) doesn't care what
  order Walk visits keys in; (2) a session temp table of seen keys + anti-join DELETE — rejected
  only because it pins one pool connection for the whole crawl for no benefit seen_at doesn't
  already give.
- race argument: cutoff is read *before* Walk starts, not merely before the sweep call. Any row
  DeleteUnseenFiles removes was last stamped strictly before that read; if the corresponding object
  were still in S3, Walk's own listing (S3 listings are strongly consistent) would have re-stamped
  it via UpsertFiles before the sweep runs, since Walk necessarily starts after cutoff was read. An
  object landing mid-walk (e.g. a concurrent CommitUpload) stamps seen_at after cutoff and survives
  either way. This is also why cutoff comes from DatabaseNow (the database's own clock) rather than
  the crawler process's clock: stamp and cutoff must share one clock or crawler/API clock skew could
  sweep a file whose CommitUpload just landed.
- the sweep is skipped entirely — not just cutoff-guarded — if DatabaseNow, Walk, or any flush
  fails: a partial listing must never be read as "everything unstamped is gone".
- directory pruning cannot be folded into DeleteUnseenFiles as a single statement (e.g. a
  data-modifying CTE piping RETURNING key into a directories DELETE): every sub-statement of one
  SQL statement runs against the same snapshot, so a prune driven off files as it stood before this
  statement's own delete would see every about-to-be-orphaned directory as still occupied and
  remove nothing. It has to be a second statement, in the same transaction, after the delete — same
  shape DeleteFileWithDirectories/DeleteFilesByIDsWithDirectories already use.
- that second statement is the existing PruneOrphanDirectories (unscoped), not DeleteUnseenFiles
  :many + the keys-scoped PruneDirectoriesForKeys, even though RETURNING key would make the scoped
  option straightforward (the old claim in PruneOrphanDirectories' doc comment that there's "no
  natural per-key list" to scope to was never actually true and has been corrected). Chose unscoped
  because an out-of-band sweep has no natural bound on victim count — a misconfigured bucket, or a
  large prefix deleted directly in S3, could sweep most of the table — and streaming that many keys
  through the crawler process is worse than one unscoped pass over directories, a table sized by
  directory count, not file count (already measured cheap at 2000+ candidates, see
  PruneOrphanDirectories' doc comment).
- deliberately no guardrails: no fraction-of-total circuit breaker, no grace period before a row
  becomes sweep-eligible, no dry-run toggle. S3 remains the single source of truth, so a sweep
  against a misconfigured bucket or a genuinely large out-of-band deletion is a cost problem, not a
  correctness one — a subsequent correct crawl fully re-populates files (and directories,
  second-order) from scratch, same as any other re-crawl, just paying for re-indexing again.
- preview blobs for swept files are deliberately still left behind, same as they already are for
  DeleteFile's best-effort cleanup — the orphaned-preview GC job flagged 7/26/26 remains unwritten. (Done — see 7/26/26 entry and cmd/preview-gc.)
  This change makes that job more valuable, not less: out-of-band deletes now actively produce
  orphaned preview objects on every sweep, not just via DeleteFile's failure path.
- known accepted consequence, not fixed here: sweeping a files row out from under an indexer that
  currently holds a `claimed` index_queue row for it makes that job's eventual CompleteIndexQueue/
  FailIndexQueue call a harmless 0-row UPDATE (the row is already gone, cascade or otherwise) —
  logged as a warning by the indexer's existing error handling, not a new failure mode introduced
  here.
- TestDirectoriesConsistencyAfterRandomMutations (8/23/26, above) gained a third random mutation
  alongside create/delete: an out-of-band sweep, implemented by reading cutoff, re-stamping every
  *other* currently-live key (simulating the rest of the bucket being re-crawled), then sweeping —
  this is the property test that has actually caught directory-maintenance bugs before, and the
  sweep is a third path that mutates directories, so it needed the same independent-oracle coverage
  the other two paths already had.

8/25/26
- CI rework, resolving the two items logged 7/3/26:
  - `tilt ci` (kind cluster + full manifest rollout) is no longer in the hot path for Go/web
    changes. It only ran because it was the one place the integration test suite + coverage gate
    lived, but every `//go:build integration` test provisions its own throwaway DB/bucket
    (internal/db/dbtest, internal/storage's setupBucket) and needs nothing else deployed — no test
    dials a live gRPC service. Integration tests + the coverage gate moved to a new
    test-integration.yml using postgres/MinIO directly (service container + docker run, no kind),
    ~8m faster per run. tilt ci is now deploy-verify.yml, gated on deploy/**, Tiltfile,
    ctlptl.yaml, Dockerfile, web/Dockerfile — a deploy-manifest check, not a test runner.
  - trivy's image scan (7-way matrix, one build per cmd/) moved off PR/push entirely, onto a
    weekly cron. All 7 targets are FROM scratch + one static binary from the same go.mod: no OS
    packages exist to scan, so the matrix only ever reported Go-binary vulns — a strict subset of
    what the filesystem scan already reports from go.sum. Not worth rebuilding 7 images on every
    PR for that. image-web (node:24-alpine, real OS packages, actual incremental coverage) stays
    on PRs, path-filtered to web/**.
  - added timeout-minutes to every job (previously only the old tilt-ci job had one; default is
    6h, so one hung job could burn 10%+ of the monthly Actions minutes on this private repo).
  - added concurrency groups (cancel-in-progress on PRs only) to every workflow.
  - Go module cache is now shared across jobs via .github/actions/setup-go-cache (content-
    addressed on go.sum, safe to share); only the build cache stays per-job-keyed. The old
    per-job module cache copies (README's prior justification) were mostly redundant storage
    against the 10GB/repo cache budget.
  - open follow-up, not done here: if this repo goes public, branch protection + merge queue
    become available, which changes two things back: (1) required status checks can't tolerate
    path-filtered workflows being skipped entirely (they'd block PRs forever waiting on a status
    that never reports), so the cheap lint/unit jobs should drop their path filters and become the
    required checks, filtering reserved for the genuinely expensive jobs; (2) Renovate's
    `platformAutomerge` should flip true once required checks exist — currently false because
    native GitHub automerge would otherwise merge without waiting on any check.

9/1/26
- per-checkout namespaces on the one shared kind cluster, reachable at
  http://<svc>.<checkout-dir>.localhost (port 80) via a shared cloud-provider-kind Gateway
  (cluster/gateway.yaml) — replaces the fixed host port-forwards (web 3000, postgres 5432, MinIO
  9000/9001, gRPC 50052/50053) that made concurrent worktrees impossible. The justfile `ns`
  variable (sanitized checkout dir name) is the single namespace derivation point; `just tilt-up`
  passes it via `tilt up --namespace` and the Tiltfile reads it back with k8s_namespace() and
  creates it via namespace_create. Objects stay namespace-free in git (same model as
  `kubectl apply -n`); S3_PUBLIC_ENDPOINT comes from the local
  overlay (deploy/overlays/local/files-public-endpoint.yaml patch — downward-API $ expansion of
  metadata.namespace; it MUST be explicit env entries, since kubelet never expands $(VAR) arriving
  via envFrom/secrets), keeping base files.yaml generic (unset = presign against the in-cluster
  endpoint, per internal/config). The gateway HTTPRoutes are generated in the Tiltfile itself — their hostnames carry the
  checkout namespace, which Gateway API cannot parameterize natively, so no committed manifest
  is token-substituted at all.
- the loopback forwarder (kind-gateway-proxy socat container, created by `just cluster-up`)
  publishes 127.0.0.1:80 ONLY, never dual-stack: *.localhost resolves to ::1 first under
  systemd-resolved, and an accept-then-reset listener on ::1 (docker-proxy in front of the
  IPv4-only envoy) makes Chrome NOT fall back to IPv4, whereas a refused ::1 connection does.
  So ::1:80 must refuse, not reset.
- teardown gotcha: cloud-provider-kind's gateway data planes are plain docker containers
  (kindccm-gw-*) on the kind network; they survive `ctlptl delete` and poison a recreated gateway
  with a stale xDS address (symptom: envoy 404s while HTTPRoutes show Accepted). `just
  cluster-down` force-removes them alongside the cloud-provider-kind/kind-gateway-proxy
  containers.
- `just down` semantics changed: it stops only this checkout's Tilt and deletes only this
  checkout's namespace (a still-running `tilt up` would re-apply what `tilt down` deletes, so
  tilt-down first pkills the session matched by its unique per-checkout UI port). `just
  cluster-down` is the explicit destroy-everything command.
- follow-up reorg (same PR): all shared-cluster bootstrap lives in top-level `cluster/` —
  ctlptl.yaml, gateway.yaml, and up.sh/down.sh extracted from the justfile (recipes are thin
  wrappers, so just --list stays self-documenting). The seed dataset AND its MinIO seed
  sidecar moved from deploy/base into deploy/overlays/local (seed/, seed.md, seed-sidecar.yaml
  strategic-merge patch): seeding is dev-only, so base MinIO is now seed-free and base no
  longer references the Tiltfile-generated seed-data ConfigMap at all. And the gateway
  HTTPRoutes moved from a $NAMESPACE-token routes.yaml substituted by the Tiltfile to being
  generated inline in the Tiltfile (plain Starlark % formatting over a (name, service, port)
  table): the routes are Tilt-scoped by nature (*.localhost, applied only by Tilt), so the
  token-file "exception" had no applyability upside and just carried a failure mode. Same
  instinct for S3_PUBLIC_ENDPOINT: moved from base files.yaml into the overlay as a patch —
  the browser-facing presign endpoint is an environment concern, and config treats it as
  optional (empty = sign against the internal endpoint), so base needs nothing. Then the rest
  of the local environment followed: postgres.yaml, minio.yaml, and BOTH generators for
  db-secret/s3-secret moved from base into overlays/local — the secrets' values point AT the
  local infra (DB_HOST=postgres, S3_ENDPOINT=local-s3:9000), so they're as env-specific as the
  infra itself. Base is now just the app: it consumes db-secret/s3-secret via envFrom but
  doesn't define them; every environment supplies its own (prod would point at real S3/DB).
  migrate and index-config stay in base (needed in every environment). Overlay needs its own
  generatorOptions.disableNameSuffixHash or the secrets get hash suffixes and every envFrom
  breaks. Each of these moves was verified by diffing `kustomize build` before/after:
  byte-identical output, zero rollouts.
