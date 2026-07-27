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
