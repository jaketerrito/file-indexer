-- +goose Up

-- pg_trgm backs the search service's text query filter on file keys: a
-- case-insensitive ILIKE substring branch and a trigram word-similarity
-- (`%>`, query within key) branch for typo tolerance. The GIN index serves
-- both.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- Serves both the ILIKE '%q%' substring branch and the key %> q
-- word-similarity branch of the ListFilesBy* queries.
CREATE INDEX IF NOT EXISTS files_key_trgm_idx ON files USING gin (key gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS files_key_trgm_idx;
DROP EXTENSION IF EXISTS pg_trgm;
