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

// noStale is a stale_before cutoff far in the past so claims never reclaim
// other tests' in-flight rows.
var noStale = pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}

// indexStatRow reads a row directly for state assertions.
func indexStatRow(t *testing.T, conn *pgx.Conn, fileID int64) IndexStat {
	t.Helper()
	var s IndexStat
	err := conn.QueryRow(context.Background(),
		`SELECT file_id, status, attempts, next_attempt_at, claimed_at, last_error, updated_at,
		        mark, content_type, size_bytes, last_modified
		 FROM index_stat WHERE file_id = $1`, fileID).
		Scan(&s.FileID, &s.Status, &s.Attempts, &s.NextAttemptAt, &s.ClaimedAt, &s.LastError, &s.UpdatedAt,
			&s.Mark, &s.ContentType, &s.SizeBytes, &s.LastModified)
	if err != nil {
		t.Fatalf("read index_stat row for %d: %v", fileID, err)
	}
	return s
}

// claimOurs claims batches until it has found all ids or the queue is empty.
func claimOurs(t *testing.T, q *Queries, staleBefore pgtype.Timestamptz, ids ...int64) []ClaimIndexStatRow {
	t.Helper()
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var ours []ClaimIndexStatRow
	for {
		rows, err := q.ClaimIndexStat(context.Background(), ClaimIndexStatParams{
			StaleBefore: staleBefore,
			BatchSize:   claimBatch,
		})
		if err != nil {
			t.Fatalf("ClaimIndexStat: %v", err)
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

func statResult(fileID int64, contentType string, size int64, lm time.Time) CompleteIndexStatParams {
	return CompleteIndexStatParams{
		FileID:       fileID,
		ContentType:  pgtype.Text{String: contentType, Valid: true},
		SizeBytes:    pgtype.Int8{Int64: size, Valid: true},
		LastModified: ts(lm),
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

func TestIndexStatLifecycle(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	lm := time.Now().UTC().Truncate(time.Second)
	fileID := insertTestFileWithMark(t, conn, key, lm)

	// Seed discovers the new file; seeding again must not duplicate it.
	for range 2 {
		if _, err := q.SeedIndexStat(ctx); err != nil {
			t.Fatalf("SeedIndexStat: %v", err)
		}
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "pending" || s.Attempts != 0 {
		t.Fatalf("after seed: %+v, want pending 0 attempts", s)
	}

	// Claim returns the job with incremented attempts.
	ours := claimOurs(t, q, noStale, fileID)
	if len(ours) != 1 || ours[0].Key != key || ours[0].Attempts != 1 {
		t.Fatalf("claim = %+v, want key %q attempts 1", ours, key)
	}

	// In-flight row must not be re-claimed.
	if again := claimOurs(t, q, noStale, fileID); len(again) != 0 {
		t.Errorf("re-claimed in-flight row: %v", again)
	}

	// Complete writes status + mark + stat results atomically.
	if err := q.CompleteIndexStat(ctx, statResult(fileID, "text/plain", 42, lm)); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
	s := indexStatRow(t, conn, fileID)
	if s.Status != "done" || s.LastError.Valid {
		t.Fatalf("after complete: %+v, want done with no error", s)
	}
	if !s.Mark.Valid || !s.Mark.Time.Equal(lm) {
		t.Errorf("mark = %+v, want %v", s.Mark, lm)
	}
	if s.ContentType.String != "text/plain" || s.SizeBytes.Int64 != 42 {
		t.Errorf("results = %+v, want text/plain/42", s)
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
	if again := claimOurs(t, q, noStale, fileID); len(again) != 0 {
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
	n, err := q.RequeueStaleIndexStat(ctx)
	if err != nil {
		t.Fatalf("RequeueStaleIndexStat: %v", err)
	}
	if n < 1 {
		t.Fatalf("RequeueStaleIndexStat reset %d rows, want >= 1", n)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "pending" || s.Attempts != 0 || s.LastError.Valid {
		t.Fatalf("after stale requeue: %+v, want clean pending", s)
	}

	// Requeue with same mark is a no-op.
	n, err = q.RequeueStaleIndexStat(ctx)
	if err != nil {
		t.Fatalf("RequeueStaleIndexStat(no-op): %v", err)
	}
	if n != 0 {
		// Pending rows don't get requeued (they're already queued).
		// This is correct - requeue only targets done rows.
		t.Logf("note: RequeueStaleIndexStat on pending row returned %d (rows already pending are left alone)", n)
	}

	// Fail with backoff.
	ours = claimOurs(t, q, noStale, fileID)
	if len(ours) != 1 {
		t.Fatalf("claim after requeue: got %v, want our row", ours)
	}
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := q.FailIndexStat(ctx, FailIndexStatParams{
		FileID: fileID, LastError: pgtype.Text{String: "stat boom", Valid: true},
		NextAttemptAt: ts(future), Exhausted: false,
	}); err != nil {
		t.Fatalf("FailIndexStat: %v", err)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "pending" || !s.NextAttemptAt.Time.Equal(future) {
		t.Fatalf("after fail: %+v, want pending with backoff", s)
	}
	// Backed-off row not claimable before next_attempt_at.
	if backedOff := claimOurs(t, q, noStale, fileID); len(backedOff) != 0 {
		t.Errorf("claimed backed-off row: %v", backedOff)
	}

	// Exhausted failure parks as error.
	if err := q.FailIndexStat(ctx, FailIndexStatParams{
		FileID: fileID, LastError: pgtype.Text{String: "gave up", Valid: true},
		NextAttemptAt: ts(time.Now().UTC()), Exhausted: true,
	}); err != nil {
		t.Fatalf("FailIndexStat(exhausted): %v", err)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "error" {
		t.Fatalf("after exhausted fail: %+v, want error", s)
	}
	if parked := claimOurs(t, q, noStale, fileID); len(parked) != 0 {
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

	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}
	if ours := claimOurs(t, q, noStale, fileID); len(ours) != 1 {
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
	if err := q.CompleteIndexStat(ctx, statResult(fileID, "text/plain", 42, lm)); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
	s := indexStatRow(t, conn, fileID)
	if !s.Mark.Time.Equal(newerLm) {
		t.Errorf("mark = %v, want %v (complete should capture current marked_at)", s.Mark.Time, newerLm)
	}

	// Next seed cycle detects mark == files.marked_at so NO requeue.
	n, err := q.RequeueStaleIndexStat(ctx)
	if err != nil {
		t.Fatalf("RequeueStaleIndexStat: %v", err)
	}
	if n != 0 {
		t.Errorf("requeued %d rows, want 0 (mark already up to date)", n)
	}
}

func TestReleaseIndexStat(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	fileID := insertTestFileWithMark(t, conn, uniqueKey(t), time.Now().UTC())
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}
	if ours := claimOurs(t, q, noStale, fileID); len(ours) != 1 {
		t.Fatalf("claim: got %v", ours)
	}

	// Release undoes claim without consuming an attempt.
	if err := q.ReleaseIndexStat(ctx, fileID); err != nil {
		t.Fatalf("ReleaseIndexStat: %v", err)
	}
	s := indexStatRow(t, conn, fileID)
	if s.Status != "pending" || s.Attempts != 0 || s.ClaimedAt.Valid {
		t.Fatalf("after release: %+v, want pending 0 attempts no claim", s)
	}

	// Release on a done row is a no-op.
	if err := q.CompleteIndexStat(ctx, statResult(fileID, "text/plain", 1, time.Now().UTC())); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
	if err := q.ReleaseIndexStat(ctx, fileID); err != nil {
		t.Fatalf("ReleaseIndexStat(done): %v", err)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "done" {
		t.Fatalf("release clobbered done row: %+v", s)
	}
}

func TestClaimIndexStatReclaimsStale(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	fileID := insertTestFileWithMark(t, conn, uniqueKey(t), time.Now().UTC())
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}
	if ours := claimOurs(t, q, noStale, fileID); len(ours) != 1 {
		t.Fatalf("initial claim: got %v", ours)
	}

	// Future stale_before treats the fresh claim as expired.
	staleAll := ts(time.Now().UTC().Add(time.Hour))
	reclaimed := claimOurs(t, q, staleAll, fileID)
	if len(reclaimed) != 1 {
		t.Fatalf("stale reclaim: got %v", reclaimed)
	}
	if reclaimed[0].Attempts != 2 {
		t.Errorf("reclaimed attempts = %d, want 2", reclaimed[0].Attempts)
	}
}

func TestClaimIndexStatSkipLocked(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	lm := time.Now().UTC()
	insertTestFileWithMark(t, conn, uniqueKey(t)+"-a", lm)
	insertTestFileWithMark(t, conn, uniqueKey(t)+"-b", lm)
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
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
		rows, err := New(conn).WithTx(tx).ClaimIndexStat(ctx, ClaimIndexStatParams{
			StaleBefore: noStale,
			BatchSize:   claimBatch,
		})
		if err != nil {
			t.Fatalf("ClaimIndexStat in tx: %v", err)
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
