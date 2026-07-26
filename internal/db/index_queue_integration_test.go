//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// claimBatch bounds one claim round trip; claimOurs loops until found.
const claimBatch = 1000

// statType is the index_type used throughout these tests; a second literal
// ("other") is used where type isolation itself is under test. This package
// runs against its own temporary database (see TestMain in
// integration_test.go), so there is no real indexer daemon or other test
// binary sharing "stat" here.
const statType = "stat"

// noStale is a huge stale_timeout so claims never reclaim other tests'
// in-flight rows.
var noStale = pgtype.Interval{Months: 1200, Days: 0, Microseconds: 0}

// indexQueueRow reads a row directly for state assertions.
func indexQueueRow(t *testing.T, conn *pgx.Conn, indexType string, fileID int64) IndexQueue {
	t.Helper()
	var s IndexQueue
	err := conn.QueryRow(context.Background(),
		`SELECT index_type, file_id, status, attempts, next_attempt_at, claimed_at, last_error, updated_at, mark
		 FROM index_queue WHERE index_type = $1 AND file_id = $2`, indexType, fileID).
		Scan(&s.IndexType, &s.FileID, &s.Status, &s.Attempts, &s.NextAttemptAt, &s.ClaimedAt, &s.LastError, &s.UpdatedAt, &s.Mark)
	if err != nil {
		t.Fatalf("read index_queue row for %s/%d: %v", indexType, fileID, err)
	}
	return s
}

// claimOurs claims batches until it has found all ids or the queue is empty.
func claimOurs(t *testing.T, q *Queries, indexType string, staleTimeout pgtype.Interval, ids ...int64) []ClaimIndexQueueRow {
	t.Helper()
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var ours []ClaimIndexQueueRow
	for {
		rows, err := q.ClaimIndexQueue(context.Background(), ClaimIndexQueueParams{
			IndexType:   indexType,
			StaleTimeout: staleTimeout,
			BatchSize:   claimBatch,
		})
		if err != nil {
			t.Fatalf("ClaimIndexQueue: %v", err)
		}
		for _, row := range rows {
			if want[row.FileID] {
				ours = append(ours, row)
			}
		}
		if len(rows) == 0 || len(ours) == len(ids) {
			return ours
		}
	}
}

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func statResult(fileID int64, contentType string, size int64, lm time.Time) UpsertIndexStatResultParams {
	return UpsertIndexStatResultParams{
		FileID:       fileID,
		ContentType:  contentType,
		SizeBytes:    size,
		LastModified: ts(lm),
	}
}

// completeStat completes a stat job: the status flip and result write are a
// transaction in production (see indexer.PGQueue.Complete); tests run them
// back-to-back since there is no concurrent writer to race with.
func completeStat(t *testing.T, q *Queries, fileID int64, contentType string, size int64, lm time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := q.CompleteIndexQueue(ctx, CompleteIndexQueueParams{IndexType: statType, FileID: fileID}); err != nil {
		t.Fatalf("CompleteIndexQueue: %v", err)
	}
	if err := q.UpsertIndexStatResult(ctx, statResult(fileID, contentType, size, lm)); err != nil {
		t.Fatalf("UpsertIndexStatResult: %v", err)
	}
}

// insertTestFileWithMark upserts one file with the given mark and returns its id.
func insertTestFileWithMark(t *testing.T, conn *pgx.Conn, key string, mark time.Time) int64 {
	t.Helper()
	q := New(conn)
	_, err := q.UpsertFiles(context.Background(), UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{ts(mark)},
	})
	if err != nil {
		t.Fatalf("UpsertFiles: %v", err)
	}
	var id int64
	if err = conn.QueryRow(context.Background(), `SELECT id FROM files WHERE key = $1`, key).Scan(&id); err != nil {
		t.Fatalf("get file id: %v", err)
	}
	t.Cleanup(func() { _, _ = q.DeleteFile(context.Background(), id) })
	return id
}

func TestUpsertFilesIdempotent(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	lm := time.Now().UTC().Truncate(time.Millisecond)
	fileID := insertTestFileWithMark(t, conn, key, lm)

	// Re-upsert with same mtime: marked_at unchanged.
	_, err := q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{ts(lm)},
	})
	if err != nil {
		t.Fatalf("UpsertFiles(same): %v", err)
	}
	row := conn.QueryRow(ctx, `SELECT marked_at FROM files WHERE id = $1`, fileID)
	var got pgtype.Timestamptz
	if err := row.Scan(&got); err != nil {
		t.Fatalf("scan marked_at: %v", err)
	}
	if !got.Time.Equal(lm) {
		t.Errorf("marked_at = %v, want %v (same mtime should not change mark)", got.Time, lm)
	}

	// Re-upsert with older mtime: GREATEST keeps the newer stored value.
	older := lm.Add(-time.Hour)
	_, err = q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{ts(older)},
	})
	if err != nil {
		t.Fatalf("UpsertFiles(older): %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT marked_at FROM files WHERE id = $1`, fileID).Scan(&got); err != nil {
		t.Fatalf("scan marked_at after older: %v", err)
	}
	if !got.Time.Equal(lm) {
		t.Errorf("marked_at = %v after older upsert, want %v (GREATEST should keep newer)", got.Time, lm)
	}

	// Re-upsert with newer mtime: marked_at is bumped.
	newer := lm.Add(time.Hour)
	_, err = q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{ts(newer)},
	})
	if err != nil {
		t.Fatalf("UpsertFiles(newer): %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT marked_at FROM files WHERE id = $1`, fileID).Scan(&got); err != nil {
		t.Fatalf("scan marked_at after newer: %v", err)
	}
	if !got.Time.Equal(newer) {
		t.Errorf("marked_at = %v after newer upsert, want %v", got.Time, newer)
	}
}

func TestIndexQueueLifecycle(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	lm := time.Now().UTC().Truncate(time.Second)
	fileID := insertTestFileWithMark(t, conn, key, lm)

	// Seed discovers the new file; seeding again must not duplicate it.
	for range 2 {
		if _, err := q.SeedIndexQueue(ctx, statType); err != nil {
			t.Fatalf("SeedIndexQueue: %v", err)
		}
	}
	if s := indexQueueRow(t, conn, statType, fileID); s.Status != "pending" || s.Attempts != 0 {
		t.Fatalf("after seed: %+v, want pending 0 attempts", s)
	}

	// Claim returns the job with incremented attempts.
	ours := claimOurs(t, q, statType, noStale, fileID)
	if len(ours) != 1 || ours[0].Key != key || ours[0].Attempts != 1 {
		t.Fatalf("claim = %+v, want key %q attempts 1", ours, key)
	}

	// In-flight row must not be re-claimed.
	if again := claimOurs(t, q, statType, noStale, fileID); len(again) != 0 {
		t.Errorf("re-claimed in-flight row: %v", again)
	}

	// Complete writes status + mark; the result write is a separate table.
	completeStat(t, q, fileID, "text/plain", 42, lm)
	s := indexQueueRow(t, conn, statType, fileID)
	if s.Status != "done" || s.LastError.Valid {
		t.Fatalf("after complete: %+v, want done with no error", s)
	}
	if !s.Mark.Valid || !s.Mark.Time.Equal(lm) {
		t.Errorf("mark = %+v, want %v", s.Mark, lm)
	}

	// file_infos view serves the metadata.
	info, err := q.GetFile(ctx, fileID)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if info.ContentType.String != "text/plain" || info.SizeBytes.Int64 != 42 {
		t.Errorf("file_infos = %+v, want stat results visible", info)
	}

	// Done rows are not claimable.
	if again := claimOurs(t, q, statType, noStale, fileID); len(again) != 0 {
		t.Errorf("claimed a done row: %v", again)
	}

	// Bump marked_at to simulate an edited object. Requeue should pick it up.
	newerLm := lm.Add(time.Hour)
	if _, err := q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{ts(newerLm)},
	}); err != nil {
		t.Fatalf("UpsertFiles(newer): %v", err)
	}
	n, err := q.RequeueStaleIndexQueue(ctx, statType)
	if err != nil {
		t.Fatalf("RequeueStaleIndexQueue: %v", err)
	}
	if n < 1 {
		t.Fatalf("RequeueStaleIndexQueue reset %d rows, want >= 1", n)
	}
	if s := indexQueueRow(t, conn, statType, fileID); s.Status != "pending" || s.Attempts != 0 || s.LastError.Valid {
		t.Fatalf("after stale requeue: %+v, want clean pending", s)
	}

	// Second requeue is a no-op: mark was cleared by the first requeue.
	n, err = q.RequeueStaleIndexQueue(ctx, statType)
	if err != nil {
		t.Fatalf("RequeueStaleIndexQueue(no-op): %v", err)
	}
	if n != 0 {
		t.Errorf("requeued %d rows, want 0 (mark already cleared)", n)
	}

	// Fail with backoff.
	ours = claimOurs(t, q, statType, noStale, fileID)
	if len(ours) != 1 {
		t.Fatalf("claim after requeue: got %v, want our row", ours)
	}
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := q.FailIndexQueue(ctx, FailIndexQueueParams{
		IndexType: statType, FileID: fileID, LastError: pgtype.Text{String: "stat boom", Valid: true},
		NextAttemptAt: ts(future), Exhausted: false,
	}); err != nil {
		t.Fatalf("FailIndexQueue: %v", err)
	}
	if s := indexQueueRow(t, conn, statType, fileID); s.Status != "pending" || !s.NextAttemptAt.Time.Equal(future) {
		t.Fatalf("after fail: %+v, want pending with backoff", s)
	}
	// Backed-off row not claimable before next_attempt_at.
	if backedOff := claimOurs(t, q, statType, noStale, fileID); len(backedOff) != 0 {
		t.Errorf("claimed backed-off row: %v", backedOff)
	}

	// Exhausted failure parks as error.
	if err := q.FailIndexQueue(ctx, FailIndexQueueParams{
		IndexType: statType, FileID: fileID, LastError: pgtype.Text{String: "gave up", Valid: true},
		NextAttemptAt: ts(time.Now().UTC()), Exhausted: true,
	}); err != nil {
		t.Fatalf("FailIndexQueue(exhausted): %v", err)
	}
	if s := indexQueueRow(t, conn, statType, fileID); s.Status != "error" {
		t.Fatalf("after exhausted fail: %+v, want error", s)
	}
	if parked := claimOurs(t, q, statType, noStale, fileID); len(parked) != 0 {
		t.Errorf("claimed errored row: %v", parked)
	}
}

func TestCompleteMarkCapture(t *testing.T) {
	// If files.marked_at is bumped *during* indexing (object edited mid-run),
	// complete captures the current marked_at, which differs from the new mark
	// so the next seed cycle detects it as stale and re-enqueues.
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	lm := time.Now().UTC().Truncate(time.Second)
	fileID := insertTestFileWithMark(t, conn, key, lm)

	if _, err := q.SeedIndexQueue(ctx, statType); err != nil {
		t.Fatalf("SeedIndexQueue: %v", err)
	}
	if ours := claimOurs(t, q, statType, noStale, fileID); len(ours) != 1 {
		t.Fatalf("claim: got %v", ours)
	}

	// Simulate edit arriving while indexing: bump marked_at.
	newerLm := lm.Add(time.Hour)
	if _, err := q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{ts(newerLm)},
	}); err != nil {
		t.Fatalf("UpsertFiles(mid-run bump): %v", err)
	}

	// Complete copies the current (bumped) marked_at.
	completeStat(t, q, fileID, "text/plain", 42, lm)
	s := indexQueueRow(t, conn, statType, fileID)
	if !s.Mark.Time.Equal(newerLm) {
		t.Errorf("mark = %v, want %v (complete should capture current marked_at)", s.Mark.Time, newerLm)
	}

	// Next seed cycle detects mark == files.marked_at so NO requeue.
	n, err := q.RequeueStaleIndexQueue(ctx, statType)
	if err != nil {
		t.Fatalf("RequeueStaleIndexQueue: %v", err)
	}
	if n != 0 {
		t.Errorf("requeued %d rows, want 0 (mark already up to date)", n)
	}
}

func TestClaimIndexQueueReclaimsStale(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	fileID := insertTestFileWithMark(t, conn, uniqueKey(t), time.Now().UTC())
	if _, err := q.SeedIndexQueue(ctx, statType); err != nil {
		t.Fatalf("SeedIndexQueue: %v", err)
	}
	if ours := claimOurs(t, q, statType, noStale, fileID); len(ours) != 1 {
		t.Fatalf("initial claim: got %v", ours)
	}

	// Zero stale_timeout treats the fresh claim as expired.
	staleAll := pgtype.Interval{Microseconds: 0}
	reclaimed := claimOurs(t, q, statType, staleAll, fileID)
	if len(reclaimed) != 1 {
		t.Fatalf("stale reclaim: got %v", reclaimed)
	}
	if reclaimed[0].Attempts != 2 {
		t.Errorf("reclaimed attempts = %d, want 2", reclaimed[0].Attempts)
	}
}

func TestClaimIndexQueueSkipLocked(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	lm := time.Now().UTC()
	insertTestFileWithMark(t, conn, uniqueKey(t)+"-a", lm)
	insertTestFileWithMark(t, conn, uniqueKey(t)+"-b", lm)
	if _, err := q.SeedIndexQueue(ctx, statType); err != nil {
		t.Fatalf("SeedIndexQueue: %v", err)
	}

	conn2, err := pgx.Connect(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("pgx.Connect: %v", err)
	}
	defer conn2.Close(ctx)

	tx1, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx)
	tx2, err := conn2.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	defer tx2.Rollback(ctx)

	claim := func(tx pgx.Tx) map[int64]bool {
		rows, err := New(conn).WithTx(tx).ClaimIndexQueue(ctx, ClaimIndexQueueParams{
			IndexType:   statType,
			StaleTimeout: noStale,
			BatchSize:   claimBatch,
		})
		if err != nil {
			t.Fatalf("ClaimIndexQueue in tx: %v", err)
		}
		got := map[int64]bool{}
		for _, r := range rows {
			got[r.FileID] = true
		}
		return got
	}

	got1 := claim(tx1)
	got2 := claim(tx2)

	for id := range got1 {
		if got2[id] {
			t.Errorf("file %d claimed by both concurrent transactions", id)
		}
	}
}

func TestIndexQueueTypeIsolation(t *testing.T) {
	// Two index types on the same file must not interfere: seeding,
	// claiming, and completing one type leaves the other untouched.
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	fileID := insertTestFileWithMark(t, conn, uniqueKey(t), time.Now().UTC())

	if _, err := q.SeedIndexQueue(ctx, statType); err != nil {
		t.Fatalf("SeedIndexQueue(stat): %v", err)
	}
	if _, err := q.SeedIndexQueue(ctx, "other"); err != nil {
		t.Fatalf("SeedIndexQueue(other): %v", err)
	}

	// Claim and complete only the stat row.
	if ours := claimOurs(t, q, statType, noStale, fileID); len(ours) != 1 {
		t.Fatalf("claim(stat): got %v", ours)
	}
	completeStat(t, q, fileID, "text/plain", 1, time.Now().UTC())

	statRow := indexQueueRow(t, conn, statType, fileID)
	otherRow := indexQueueRow(t, conn, "other", fileID)
	if statRow.Status != "done" {
		t.Errorf("stat row = %+v, want done", statRow)
	}
	if otherRow.Status != "pending" {
		t.Errorf("other row = %+v, want still pending (unaffected by stat completion)", otherRow)
	}

	// "other" type's row is still claimable independently of stat's.
	if ours := claimOurs(t, q, "other", noStale, fileID); len(ours) != 1 {
		t.Errorf("claim(other): got %v, want the untouched row", ours)
	}
}
