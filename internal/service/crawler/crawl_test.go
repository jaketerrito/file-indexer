package crawler

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

// cutoffTime is the fixed DatabaseNow value used by expectSweep and its
// callers; TestRunSweepUsesCutoffFromDatabaseNow checks it against the
// value DeleteUnseenFilesWithDirectories actually receives.
var cutoffTime = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

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

// expectSweep sets up the end-of-crawl DatabaseNow + sweep pass (see Run's
// doc comment): every successful crawl runs it exactly once, regardless of
// whether anything was discovered, deleting deleted rows.
func expectSweep(files *MockFileStore, deleted int64) {
	files.EXPECT().DatabaseNow(mock.Anything).
		Return(pgtype.Timestamptz{Time: cutoffTime, Valid: true}, nil)
	files.EXPECT().
		DeleteUnseenFilesWithDirectories(mock.Anything, cutoffTime).
		Return(deleted, nil)
}

func TestRun(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a"), objectInfo("b")))

	files := NewMockFileStore(t)
	files.EXPECT().
		UpsertFilesWithDirectories(mock.Anything, mock.MatchedBy(upsertMatcher("a", "b"))).
		Return(2, nil)
	expectSweep(files, 0)

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
	expectSweep(files, 0)

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
	expectSweep(files, 0)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunEmptyBucket(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).RunAndReturn(walkOver())

	files := NewMockFileStore(t)
	// The sweep runs even when nothing was discovered this crawl — an empty
	// listing is exactly the case where everything previously known should
	// be swept (bucket genuinely emptied out of band).
	expectSweep(files, 0)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	files.AssertNotCalled(t, "UpsertFilesWithDirectories", mock.Anything, mock.Anything)
}

// TestRunSweepDeletesUnseenFiles is the one test that exercises an actual
// out-of-band delete: Walk reports fewer objects than a previous crawl
// would have, and the sweep's return value propagates into Run's summary
// log (observed here only via the returned nil error — the log line itself
// isn't asserted, deleted count plumbing is covered by the mock's return
// value flowing through untouched).
func TestRunSweepDeletesUnseenFiles(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a")))

	files := NewMockFileStore(t)
	files.EXPECT().UpsertFilesWithDirectories(mock.Anything, mock.MatchedBy(upsertMatcher("a"))).Return(1, nil)
	expectSweep(files, 3)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestRunSweepUsesCutoffFromDatabaseNow pins that the cutoff passed to
// DeleteUnseenFilesWithDirectories is exactly whatever DatabaseNow
// returned, not e.g. a value computed from the crawler process's own
// clock — see DeleteUnseenFiles' doc comment for why that distinction
// matters (clock-skew safety).
func TestRunSweepUsesCutoffFromDatabaseNow(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).RunAndReturn(walkOver())

	distinctCutoff := time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)
	files := NewMockFileStore(t)
	files.EXPECT().DatabaseNow(mock.Anything).
		Return(pgtype.Timestamptz{Time: distinctCutoff, Valid: true}, nil)
	files.EXPECT().
		DeleteUnseenFilesWithDirectories(mock.Anything, distinctCutoff).
		Return(0, nil)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestRunDatabaseNowReadBeforeWalk pins the ordering DeleteUnseenFiles'
// race argument depends on: the cutoff must be read before Walk starts, not
// merely before the sweep call, since a Walk that observes an object
// re-stamps it — a cutoff read after Walk could sit after that re-stamp and
// wrongly sweep a file the crawl itself just confirmed still exists.
func TestRunDatabaseNowReadBeforeWalk(t *testing.T) {
	var order []string

	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(storage.ObjectInfo) error) error {
			order = append(order, "walk")
			return nil
		})

	files := NewMockFileStore(t)
	files.EXPECT().DatabaseNow(mock.Anything).
		RunAndReturn(func(ctx context.Context) (pgtype.Timestamptz, error) {
			order = append(order, "databaseNow")
			return pgtype.Timestamptz{Time: cutoffTime, Valid: true}, nil
		})
	files.EXPECT().DeleteUnseenFilesWithDirectories(mock.Anything, cutoffTime).Return(0, nil)

	if err := New(store, files, "").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "databaseNow" || order[1] != "walk" {
		t.Fatalf("call order = %v, want [databaseNow walk]", order)
	}
}

func TestRunDatabaseNowError(t *testing.T) {
	store := NewMockObjectStore(t)

	files := NewMockFileStore(t)
	files.EXPECT().DatabaseNow(mock.Anything).
		Return(pgtype.Timestamptz{}, errors.New("db error"))

	if err := New(store, files, "").Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	store.AssertNotCalled(t, "Walk", mock.Anything, mock.Anything)
}

func TestRunUpsertError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo("a")))

	files := NewMockFileStore(t)
	files.EXPECT().DatabaseNow(mock.Anything).
		Return(pgtype.Timestamptz{Time: cutoffTime, Valid: true}, nil)
	files.EXPECT().UpsertFilesWithDirectories(mock.Anything, mock.Anything).Return(0, errors.New("db error"))
	// Run returns before the sweep when the flush itself fails: an
	// incomplete listing must never be read as "everything unstamped is
	// gone".

	if err := New(store, files, "").Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	files.AssertNotCalled(t, "DeleteUnseenFilesWithDirectories", mock.Anything, mock.Anything)
}

func TestRunWalkError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).Return(errors.New("walk failed"))

	files := NewMockFileStore(t)
	files.EXPECT().DatabaseNow(mock.Anything).
		Return(pgtype.Timestamptz{Time: cutoffTime, Valid: true}, nil)

	if err := New(store, files, "").Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	files.AssertNotCalled(t, "UpsertFilesWithDirectories", mock.Anything, mock.Anything)
	files.AssertNotCalled(t, "DeleteUnseenFilesWithDirectories", mock.Anything, mock.Anything)
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
	expectSweep(files, 0)

	if err := New(store, files, ".index/").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunSkipsEveryObject(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(objectInfo(".index/previews/1")))

	files := NewMockFileStore(t)
	expectSweep(files, 0)

	if err := New(store, files, ".index/").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	files.AssertNotCalled(t, "UpsertFilesWithDirectories", mock.Anything, mock.Anything)
}

func TestRunSweepError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).RunAndReturn(walkOver())

	files := NewMockFileStore(t)
	files.EXPECT().DatabaseNow(mock.Anything).
		Return(pgtype.Timestamptz{Time: cutoffTime, Valid: true}, nil)
	files.EXPECT().DeleteUnseenFilesWithDirectories(mock.Anything, cutoffTime).
		Return(int64(0), errors.New("db error"))

	if err := New(store, files, "").Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
