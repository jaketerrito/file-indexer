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


TODO:
1. Update crawler to read s3
1. Update indexer to handle by reference rather than stream
