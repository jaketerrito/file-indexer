package crawler

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

// walkOver returns a RunAndReturn implementation that invokes the supplied
// callback once per object, mimicking a real ObjectStore.Walk. It returns
// the first callback error, matching the production loop's short-circuit
// behavior.
func walkOver(infos ...storage.ObjectInfo) func(ctx context.Context, fn func(storage.ObjectInfo) error) error {
	return func(ctx context.Context, fn func(storage.ObjectInfo) error) error {
		for _, info := range infos {
			if err := fn(info); err != nil {
				return err
			}
		}
		return nil
	}
}

func objectInfo(key string, size int64) storage.ObjectInfo {
	return storage.ObjectInfo{Key: key, Size: size, LastModified: time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)}
}

func TestRun(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a", 1), objectInfo("b", 2)))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFiles(mock.Anything, mock.MatchedBy(func(arg db.UpsertFilesParams) bool {
		return len(arg.Keys) == 2 && arg.Keys[0] == "a" && arg.Keys[1] == "b" &&
			arg.Sizes[0] == 1 && arg.Sizes[1] == 2 &&
			arg.LastModifieds[0].Valid && arg.LastModifieds[1].Valid
	})).Return([]int64{10, 11}, nil)
	files.EXPECT().ResetIndexStat(mock.Anything, []int64{10, 11}).Return(2, nil)

	c := New(store, files)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunFlushesFullBatches(t *testing.T) {
	// Three objects with batch size two: expect a flush of two then a final
	// flush of one.
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a", 1), objectInfo("b", 2), objectInfo("c", 3)))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFiles(mock.Anything, mock.MatchedBy(func(arg db.UpsertFilesParams) bool {
		return len(arg.Keys) == 2 && arg.Keys[0] == "a" && arg.Keys[1] == "b"
	})).Return([]int64{1, 2}, nil)
	files.EXPECT().UpsertFiles(mock.Anything, mock.MatchedBy(func(arg db.UpsertFilesParams) bool {
		return len(arg.Keys) == 1 && arg.Keys[0] == "c"
	})).Return([]int64{3}, nil)
	files.EXPECT().ResetIndexStat(mock.Anything, []int64{1, 2}).Return(2, nil)
	files.EXPECT().ResetIndexStat(mock.Anything, []int64{3}).Return(1, nil)

	c := New(store, files)
	c.batchSize = 2
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunUnchangedFilesSkipReset(t *testing.T) {
	// Upsert reporting no new/changed ids must not call ResetIndexStat.
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a", 1)))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFiles(mock.Anything, mock.Anything).Return(nil, nil)

	c := New(store, files)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	files.AssertNotCalled(t, "ResetIndexStat", mock.Anything, mock.Anything)
}

func TestRunEmptyBucket(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).RunAndReturn(walkOver())

	files := NewMockFileStore(t)

	c := New(store, files)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	files.AssertNotCalled(t, "UpsertFiles", mock.Anything, mock.Anything)
}

func TestRunUpsertError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a", 1)))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFiles(mock.Anything, mock.Anything).Return(nil, errors.New("upsert failed"))

	c := New(store, files)
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResetError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a", 1)))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFiles(mock.Anything, mock.Anything).Return([]int64{1}, nil)
	files.EXPECT().ResetIndexStat(mock.Anything, []int64{1}).Return(0, errors.New("reset failed"))

	c := New(store, files)
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunWalkError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).Return(errors.New("walk failed"))

	// The store must never be written when Walk itself fails.
	files := NewMockFileStore(t)

	c := New(store, files)
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	files.AssertNotCalled(t, "UpsertFiles", mock.Anything, mock.Anything)
}
