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

6/20/26
- Integration tests
- coverage gate currently excludes storage/s3.go and db/migrate.go (thin minio/goose
  wrappers) since they're only exercised by build-tagged integration tests. Once the
  integration job runs in CI, fold its coverage into the report and drop those
  exclusions so the wrappers are measured instead of ignored.

TODO:
- Frontend
