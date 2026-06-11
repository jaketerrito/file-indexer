A web app for interacting with a filesystem 

# Components

## filesystem
Support for s3 like file system (since s3 is cheap).
Support for local filesystem would be nice to have, potentially can implement this and use fuse for s3 compatability

Backups should be handled via external system on file system level.

## Web App
Web application should support:
- create, read, update and deletion of files
- searching files based off of metadata

Direct upload with presigned urls?
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
- pushed based (file is streamed/sent over in the request)
   - indexer runs in same cluster as the crud api (it can save file to disk but pass on to indexer)
   - if file is to big for streaming, can use some method to try again?
   - makes auth situation simpler
   - Allows for indexer to handle results for multiple crawlers on different file systems
- generates metadata for that file, updates database
- should be triggered based off web app updates (event queue)
- should be triggered based off of filesystem scan for unindexed files

Should be able to handle out of band uploads to file system directly (not through api), can have file system crawling script

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

# Plan
1. crawler script for filesystem
1. standalone api for indexing
1. metadata gen functions
1. build database
1. search method
1. standalone api with crud and search
1. web client that relies on the api
1. Integrate indexing triggered via the api (using event queue)
1. add more complex metadata
1. kubernetes deployment
