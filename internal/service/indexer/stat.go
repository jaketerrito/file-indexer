package indexer

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"file-indexer/internal/worker"

	"github.com/jackc/pgx/v5/pgtype"
)

// ObjectStore is the slice of storage.Storage the stat handler depends on.
type ObjectStore interface {
	Stat(ctx context.Context, key string) (storage.ObjectInfo, error)
}

// FileIndex is the slice of db.Queries the stat handler depends on.
type FileIndex interface {
	UpdateFileMetadata(ctx context.Context, arg db.UpdateFileMetadataParams) error
}

// StatIndexer handles stat jobs: it fetches object metadata from storage and
// records it on the files row. It is safe for concurrent use by multiple
// pool workers.
type StatIndexer struct {
	storage ObjectStore
	queries FileIndex
}

// NewStatIndexer constructs a StatIndexer with its dependencies already
// built by the caller (composition root).
func NewStatIndexer(store ObjectStore, queries FileIndex) *StatIndexer {
	return &StatIndexer{storage: store, queries: queries}
}

// Handle implements worker.Handler.
func (s *StatIndexer) Handle(ctx context.Context, job worker.Job) error {
	info, err := s.storage.Stat(ctx, job.Key)
	if err != nil {
		return err
	}

	return s.queries.UpdateFileMetadata(ctx, db.UpdateFileMetadataParams{
		ID:          job.FileID,
		ContentType: pgtype.Text{String: info.ContentType, Valid: true},
		SizeBytes:   pgtype.Int8{Int64: info.Size, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: info.LastModified, Valid: true},
	})
}
