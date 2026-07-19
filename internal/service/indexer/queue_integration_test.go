//go:build integration

package indexer

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/db/dbtest"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These exercise PGQueue against a real Postgres and a real transaction,
// covering the parts unit tests (which mock the Queries interface) cannot:
// NewPGQueue's wiring and Complete's Begin/CompleteIndexQueue/store/Commit
// sequence. See internal/db's integration suite for the underlying SQL
// query behavior (claim disjointness, TTL reclaim, staleness, etc).
//
// TestMain gives this package its own temporary database (see
// internal/db/dbtest), so index_type can be the plain production literal
// throughout; there is no other test binary or daemon sharing it.
func TestMain(m *testing.M) {
	os.Exit(dbtest.Main(m, db.RunMigrations))
}

// testPool returns a pgxpool.Pool to the test database. Migrations already
// ran once in TestMain.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbtest.DSN())
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertTestFile(t *testing.T, pool *pgxpool.Pool, key string, mark time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	if _, err := q.UpsertFiles(ctx, db.UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{{Time: mark, Valid: true}},
	}); err != nil {
		t.Fatalf("UpsertFiles: %v", err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM files WHERE key = $1`, key).Scan(&id); err != nil {
		t.Fatalf("get file id: %v", err)
	}
	t.Cleanup(func() { _, _ = q.DeleteFile(context.Background(), id) })
	return id
}

func TestPGQueueEndToEnd(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	indexType := "stat"

	key := fmt.Sprintf("it/%s/%d", t.Name(), time.Now().UnixNano())
	mark := time.Now().UTC().Truncate(time.Second)
	fileID := insertTestFile(t, pool, key, mark)

	queue := NewPGQueue(pool, indexType, StoreStatResult)

	// Seed discovers the new file for this index type.
	n, err := queue.Seed(ctx)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n < 1 {
		t.Fatalf("Seed = %d, want >= 1", n)
	}

	// Claim returns exactly our job.
	jobs, err := queue.Claim(ctx, 10, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	var job *Job
	for i := range jobs {
		if jobs[i].FileID == fileID {
			job = &jobs[i]
		}
	}
	if job == nil || job.Key != key || job.Attempts != 1 {
		t.Fatalf("Claim did not return our job: %+v", jobs)
	}

	// Complete must atomically flip the queue row to done AND write the
	// result row: verify both via the file_infos view, which only shows
	// non-null metadata once both halves of the transaction landed.
	result := StatResult{ContentType: "text/plain", SizeBytes: 123, LastModified: mark}
	if err := queue.Complete(ctx, job.FileID, result); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	info, err := db.New(pool).GetFile(ctx, fileID)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if info.ContentType.String != result.ContentType || !info.ContentType.Valid {
		t.Errorf("ContentType = %+v, want %q", info.ContentType, result.ContentType)
	}
	if info.SizeBytes.Int64 != result.SizeBytes || !info.SizeBytes.Valid {
		t.Errorf("SizeBytes = %+v, want %d", info.SizeBytes, result.SizeBytes)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM index_queue WHERE index_type = $1 AND file_id = $2`,
		indexType, fileID).Scan(&status); err != nil {
		t.Fatalf("read index_queue status: %v", err)
	}
	if status != "done" {
		t.Errorf("index_queue status = %q, want done", status)
	}

	// Done rows are no longer claimable.
	again, err := queue.Claim(ctx, 10, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Claim after complete: %v", err)
	}
	for _, j := range again {
		if j.FileID == fileID {
			t.Errorf("claimed a done row: %+v", j)
		}
	}
}

func TestPGQueueCompleteRollsBackOnStoreError(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	indexType := "stat"

	key := fmt.Sprintf("it/%s/%d", t.Name(), time.Now().UnixNano())
	fileID := insertTestFile(t, pool, key, time.Now().UTC())

	failingStore := func(context.Context, *db.Queries, int64, StatResult) error {
		return fmt.Errorf("store boom")
	}
	queue := NewPGQueue(pool, indexType, failingStore)

	if _, err := queue.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	jobs, err := queue.Claim(ctx, 10, time.Now().Add(-time.Hour))
	if err != nil || len(jobs) != 1 {
		t.Fatalf("Claim: jobs=%v err=%v", jobs, err)
	}

	err = queue.Complete(ctx, fileID, StatResult{})
	if err == nil {
		t.Fatal("Complete: want error from failing store")
	}

	// The queue status flip must have been rolled back with the failed
	// store write: the row stays claimed, not done.
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM index_queue WHERE index_type = $1 AND file_id = $2`,
		indexType, fileID).Scan(&status); err != nil {
		t.Fatalf("read index_queue status: %v", err)
	}
	if status != "claimed" {
		t.Errorf("index_queue status = %q, want claimed (rolled back)", status)
	}
}

func TestPGQueueFailPersists(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	indexType := "stat"

	key := fmt.Sprintf("it/%s/%d", t.Name(), time.Now().UnixNano())
	fileID := insertTestFile(t, pool, key, time.Now().UTC())

	queue := NewPGQueue(pool, indexType, StoreStatResult)
	if _, err := queue.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if _, err := queue.Claim(ctx, 10, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	next := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := queue.Fail(ctx, fileID, "boom", next, false); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	var status, lastError string
	var nextAttempt time.Time
	if err := pool.QueryRow(ctx,
		`SELECT status, last_error, next_attempt_at FROM index_queue WHERE index_type = $1 AND file_id = $2`,
		indexType, fileID).Scan(&status, &lastError, &nextAttempt); err != nil {
		t.Fatalf("read index_queue row: %v", err)
	}
	if status != "pending" || lastError != "boom" || !nextAttempt.Equal(next) {
		t.Errorf("after Fail: status=%q lastError=%q nextAttempt=%v, want pending/boom/%v",
			status, lastError, nextAttempt, next)
	}
}
