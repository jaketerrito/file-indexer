package indexer

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// StatResult is the stat index type's output: basic object metadata from S3
// StatObject.
type StatResult struct {
	ContentType  string
	SizeBytes    int64
	LastModified time.Time
}

// ObjectStore is the slice of storage.Storage the stat index type depends
// on.
type ObjectStore interface {
	Stat(ctx context.Context, key string) (storage.ObjectInfo, error)
}

// StatIndexer computes the stat index type's results.
type StatIndexer struct {
	storage ObjectStore
}

// NewStatIndexer constructs a StatIndexer with its dependencies already
// built by the caller (composition root).
func NewStatIndexer(store ObjectStore) *StatIndexer {
	return &StatIndexer{storage: store}
}

// Process implements ProcessFunc[StatResult]: fetch object metadata from
// storage. Safe to run concurrently across any number of pods.
func (s *StatIndexer) Process(ctx context.Context, job Job) (StatResult, error) {
	info, err := s.storage.Stat(ctx, job.Key)
	if err != nil {
		return StatResult{}, err
	}

	return StatResult{
		ContentType:  info.ContentType,
		SizeBytes:    info.Size,
		LastModified: info.LastModified,
	}, nil
}

// StoreStatResult is the stat index type's StoreFunc: persists the result
// row inside Complete's transaction.
func StoreStatResult(ctx context.Context, q *db.Queries, fileID int64, result StatResult) error {
	return q.UpsertIndexStatResult(ctx, db.UpsertIndexStatResultParams{
		FileID:       fileID,
		ContentType:  result.ContentType,
		SizeBytes:    result.SizeBytes,
		LastModified: pgtype.Timestamptz{Time: result.LastModified, Valid: true},
	})
}
