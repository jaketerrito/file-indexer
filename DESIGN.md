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
- takes in file name, generates metadata for that file, updates database
- should be triggered based off web app updates (event queue)
- should be triggered based off of filesystem scan for unindexed files

Should be able to handle out of band uploads to file system directly (not through api)
Reads file directly from storage

Will want seperate process/application for handling more heavy duty indexing...
- AI based tagging
- Image previews

Want indexing to be easily expandable with new indices

Indexer can be parameterized to generate different sets of metadata

# Principles
- eventually consistent, fine if web app is not totally in sync with file system
- filesystem as source of truth
- filesystem interactions need to be reliable (no lost data when changing a file)
- idempotent indexing

# Plan
1. index script for filesystem, metadata gen functions, search method
1. api with crud and search
1. web client that relies on the api
1. add more complex metadata
1. Integrate indexing triggered via the api (using event queue)
1. kubernetes deployment
