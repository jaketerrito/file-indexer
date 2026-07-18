package indexer

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"file-indexer/internal/worker"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

func TestStatIndexerHandle(t *testing.T) {
	now := time.Now()
	info := storage.ObjectInfo{
		Key:          "obj-key",
		Size:         100,
		ContentType:  "text/plain",
		LastModified: now,
	}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	queries := NewMockFileIndex(t)
	queries.EXPECT().
		UpdateFileMetadata(mock.Anything, mock.MatchedBy(func(arg db.UpdateFileMetadataParams) bool {
			return arg.ID == 42 &&
				arg.ContentType.String == "text/plain" && arg.ContentType.Valid &&
				arg.SizeBytes.Int64 == 100 && arg.SizeBytes.Valid &&
				arg.UpdatedAt.Time.Equal(now) && arg.UpdatedAt.Valid
		})).
		Return(nil)

	s := NewStatIndexer(store, queries)
	if err := s.Handle(context.Background(), worker.Job{FileID: 42, Key: "obj-key"}); err != nil {
		t.Fatal(err)
	}
}

func TestStatIndexerHandleStatError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "missing").Return(storage.ObjectInfo{}, errors.New("not found"))

	// UpdateFileMetadata must never be called when Stat fails.
	queries := NewMockFileIndex(t)

	s := NewStatIndexer(store, queries)
	if err := s.Handle(context.Background(), worker.Job{FileID: 1, Key: "missing"}); err == nil {
		t.Fatal("expected error")
	}
	queries.AssertNotCalled(t, "UpdateFileMetadata", mock.Anything, mock.Anything)
}

func TestStatIndexerHandleUpdateError(t *testing.T) {
	info := storage.ObjectInfo{Key: "obj-key", Size: 1, ContentType: "text/plain", LastModified: time.Now()}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	queries := NewMockFileIndex(t)
	queries.EXPECT().UpdateFileMetadata(mock.Anything, mock.Anything).Return(errors.New("db error"))

	s := NewStatIndexer(store, queries)
	if err := s.Handle(context.Background(), worker.Job{FileID: 1, Key: "obj-key"}); err == nil {
		t.Fatal("expected error")
	}
}
