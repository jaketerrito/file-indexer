package indexer

import (
	"context"
	"file-indexer/internal/storage"
	"file-indexer/internal/worker"
	"time"
)

// StatResult is the stat index type's output: basic object metadata from S3
// StatObject, persisted onto the index_stat row when a job completes.
type StatResult struct {
	ContentType  string
	SizeBytes    int64
	LastModified time.Time
}

// ObjectStore is the slice of storage.Storage the stat handler depends on.
type ObjectStore interface {
	Stat(ctx context.Context, key string) (storage.ObjectInfo, error)
}

// StatIndexer handles stat jobs: it fetches object metadata from storage
// and returns it for the queue to persist. It is safe for concurrent use by
// multiple pool workers.
type StatIndexer struct {
	storage ObjectStore
}

// NewStatIndexer constructs a StatIndexer with its dependencies already
// built by the caller (composition root).
func NewStatIndexer(store ObjectStore) *StatIndexer {
	return &StatIndexer{storage: store}
}

// Handle implements worker.Handler[StatResult].
func (s *StatIndexer) Handle(ctx context.Context, job worker.Job) (StatResult, error) {
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
