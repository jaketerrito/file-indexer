-- +goose Up

-- seen_at is a liveness mark, distinct from marked_at (object mtime, used to
-- detect edits for re-indexing): an object whose content never changes still
-- needs its liveness re-affirmed on every crawl, but its mtime never
-- advances, so marked_at cannot double as this. UpsertFiles stamps seen_at =
-- now() on every insert and every re-upsert (default covers the insert
-- case); a crawl that lists the whole bucket without error therefore
-- refreshes seen_at for everything still in S3. Rows whose seen_at predates
-- the start of the most recent complete crawl are gone from S3 out of band
-- (see DeleteUnseenFiles in queries/files.sql) and are swept.
ALTER TABLE files ADD COLUMN seen_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE files DROP COLUMN seen_at;
