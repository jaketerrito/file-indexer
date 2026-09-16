A web app for interacting with a filesystem 

# Components

## filesystem
Support for s3 like file system (since s3 is cheap).
S3 is the single store and source of truth for content — everything indexed must live there.

Backups should be handled via external system on file system level.

## Web App
Web application should support:
- create, read, update and deletion of files
- searching files based off of metadata

client gets Presigned urls for upload, then it needs to make a call to index
Use BFF

## DB
Database for search
- tables with file path and basic info
  - file id
  - path
  - content type
  - created at
  - updated at
  - checksum?
- seperate metadata table (single or per metadata) to allow for independent updates
  - file id
  - value
postgres would probably be fine for simple lookups

## Indexing
- reference based: index requests carry only (source, s3 path/key) — no file bytes
   - indexer GETs the object from storage and computes all metadata (size, checksum, MIME, EXIF, etc.)
   - all ingestion paths emit the same minimal reference message:
     - api emits after successful s3 commit (no phantom index entries)
     - uploader CLI goes through the api (stream bytes to api -> s3, api emits reference)
     - reconciliation crawler emits references for discovered files
   - costs one GET per file at index time, negligible when indexer is co-located with storage (same cluster/region)
   - pull path is required anyway: re-indexing, new index types added later, and crawler-discovered files all need to fetch from s3
- remote sources (laptop, other apps) are uploaders, not crawlers — everything must land in s3 first or the web app can't serve it later; existing gRPC streaming code is repurposed for the uploader -> api leg
- generates metadata for that file, updates database
- should be triggered based off web app updates (event queue)
- should be triggered based off of filesystem scan for unindexed files

Large file uploads: streaming itself has no size limit (chunked, constant memory); the real problem is interrupted long uploads
- mitigate with write-to-temp-key + commit-on-complete for atomicity

Should be able to handle out of band access/manipulation of s3 directly (not through api): crawler reconciles by listing s3, diffing against the DB, and emitting references for unindexed/changed/removed files

Will want seperate process/application for handling more heavy duty indexing...
- AI based tagging
- Image previews

Want indexing to be easily expandable with new indices

Indexer can be parameterized to generate different sets of metadata

## Search
- simple sort by date/name/etc fetch paginated
- search methods for different metadata categories
  - string match name
  - date range created date, updated date
  - string match tags

# Principles
- eventually consistent, fine if web app is not totally in sync with file system
- filesystem as source of truth
- filesystem interactions need to be reliable (no lost data when changing a file)
- idempotent indexing
