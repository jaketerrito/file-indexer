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
// callback once per object, mimicking a real ObjectStore.Walk.
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

func objectInfo(key string) storage.ObjectInfo {
	return storage.ObjectInfo{Key: key, Size: 1, LastModified: time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)}
}

// upsertMatcher returns a mock matcher that asserts the UpsertFilesParams
// contains exactly the given keys (order-sensitive) and that all
// MarkedAts are valid.
func upsertMatcher(keys ...string) func(db.UpsertFilesParams) bool {
	return func(arg db.UpsertFilesParams) bool {
		if len(arg.Keys) != len(keys) {
			return false
		}
		for i, k := range keys {
			if arg.Keys[i] != k {
				return false
			}
		}
		for _, m := range arg.MarkedAts {
			if !m.Valid {
				return false
			}
		}
		return true
	}
}

// expectReconcile sets up the end-of-crawl PruneOrphanDirectories pass (see
// Run's doc comment): every successful crawl runs it exactly once,
// regardless of whether anything was discovered.
func expectReconcile(files *MockFileStore) {
	files.EXPECT().PruneOrphanDirectories(mock.Anything).Return(nil)
}

func TestRun(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a"), objectInfo("b")))

	files := NewMockFileStore(t)
	files.EXPECT().
		UpsertFilesWithDirectories(mock.Anything, mock.MatchedBy(upsertMatcher("a", "b"))).
		Return(2, nil)
	expectReconcile(files)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunFlushesFullBatches(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a"), objectInfo("b"), objectInfo("c")))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFilesWithDirectories(mock.Anything, mock.MatchedBy(upsertMatcher("a", "b"))).Return(2, nil)
	files.EXPECT().UpsertFilesWithDirectories(mock.Anything, mock.MatchedBy(upsertMatcher("c"))).Return(1, nil)
	expectReconcile(files)

	c := New(store, files, "")
	c.batchSize = 2
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunPassesDuplicateKeysToBatch(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a"), objectInfo("a"), objectInfo("b")))

	files := NewMockFileStore(t)
	// SQL-level dedup via GROUP BY handles in-batch duplicates now.
	files.EXPECT().UpsertFilesWithDirectories(mock.Anything, mock.MatchedBy(upsertMatcher("a", "a", "b"))).Return(1, nil)
	expectReconcile(files)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunEmptyBucket(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).RunAndReturn(walkOver())

	files := NewMockFileStore(t)
	// The reconcile pass runs even when nothing was discovered this crawl —
	// it also repairs drift from any earlier cause, not just this crawl's
	// own writes.
	expectReconcile(files)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	files.AssertNotCalled(t, "UpsertFilesWithDirectories", mock.Anything, mock.Anything)
}

func TestRunUpsertError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a")))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFilesWithDirectories(mock.Anything, mock.Anything).Return(0, errors.New("db error"))
	// Run returns before the reconcile pass when the flush itself fails.

	if err := New(store, files, "").Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	files.AssertNotCalled(t, "PruneOrphanDirectories", mock.Anything)
}

func TestRunWalkError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).Return(errors.New("walk failed"))

	files := NewMockFileStore(t)

	if err := New(store, files, "").Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	files.AssertNotCalled(t, "UpsertFilesWithDirectories", mock.Anything, mock.Anything)
	files.AssertNotCalled(t, "PruneOrphanDirectories", mock.Anything)
}

func TestRunSkipsIgnoredPrefix(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(
			objectInfo("a"),
			objectInfo(".index/previews/1"),
			objectInfo(".index/previews/2"),
			// Not under the prefix: the trailing slash makes this a path
			// boundary, not a substring match.
			objectInfo(".indexnotours"),
			objectInfo("b"),
		))

	files := NewMockFileStore(t)
	files.EXPECT().
		UpsertFilesWithDirectories(mock.Anything, mock.MatchedBy(upsertMatcher("a", ".indexnotours", "b"))).
		Return(3, nil)
	expectReconcile(files)

	if err := New(store, files, ".index/").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunSkipsEveryObject(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo(".index/previews/1")))

	files := NewMockFileStore(t)
	expectReconcile(files)

	if err := New(store, files, ".index/").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	files.AssertNotCalled(t, "UpsertFilesWithDirectories", mock.Anything, mock.Anything)
}

func TestRunPruneOrphanDirectoriesError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).RunAndReturn(walkOver())

	files := NewMockFileStore(t)
	files.EXPECT().PruneOrphanDirectories(mock.Anything).Return(errors.New("db error"))

	if err := New(store, files, "").Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
