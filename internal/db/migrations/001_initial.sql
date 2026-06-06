-- +goose Up
CREATE TABLE IF NOT EXISTS files (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path       TEXT NOT NULL UNIQUE,
    content_type TEXT,
    size_bytes BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_files_path ON files (path);

-- +goose Down
DROP TABLE IF EXISTS files;
