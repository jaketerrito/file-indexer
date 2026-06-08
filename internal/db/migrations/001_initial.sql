-- +goose Up
CREATE TABLE IF NOT EXISTS files (
    id          BIGSERIAL PRIMARY KEY,
    source      TEXT NOT NULL,
    path        TEXT NOT NULL,
    content_type TEXT,
    size_bytes  BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source, path)
);

-- +goose Down
DROP TABLE IF EXISTS files;
