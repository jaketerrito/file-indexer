//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// claimBatch bounds one claim round trip; claimOurs keeps claiming batches
// until it has found the caller's rows or the queue is drained, so the size
// only affects round trips, not correctness.
const claimBatch = 1000

// noStale is a stale_before cutoff far in the past, so claims never reclaim
// other tests' in-flight rows.
var noStale = pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}

// indexStatRow reads a row directly; the production queries deliberately
// expose no point read, but tests need to observe state transitions.
func indexStatRow(t *testing.T, conn *pgx.Conn, fileID int64) IndexStat {
	t.Helper()
	var s IndexStat
	err := conn.QueryRow(context.Background(),
		`SELECT file_id, status, attempts, next_attempt_at, claimed_at, last_error, updated_at,
		        content_type, size_bytes, last_modified
		 FROM index_stat WHERE file_id = $1`, fileID).
		Scan(&s.FileID, &s.Status, &s.Attempts, &s.NextAttemptAt, &s.ClaimedAt, &s.LastError, &s.UpdatedAt,
			&s.ContentType, &s.SizeBytes, &s.LastModified)
	if err != nil {
		t.Fatalf("read index_stat row for %d: %v", fileID, err)
	}
	return s
}

// claimOurs claims batches until it has seen all of ids or the queue is
// empty, returning only the rows belonging to this test. Looping (rather
// than one huge batch) keeps the tests correct against a shared dev
// database with an arbitrary pending backlog; rows claimed incidentally
// stay claimed, which other tests tolerate because they also filter to
// their own ids.
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

func TestInsertFilesIdempotent(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	id := insertTestFile(t, q, key)

	// Re-inserting the same key returns nothing (identity already known).
	again, err := q.InsertFiles(ctx, []string{key})
	if err != nil {
		t.Fatalf("InsertFiles(duplicate): %v", err)
	}
	if len(again) != 0 {
		t.Errorf("InsertFiles(duplicate) = %v, want no ids", again)
	}

	// Discovery time is set by the database, not the crawler.
	got, err := q.GetFile(ctx, id)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !got.CreatedAt.Valid {
		t.Error("created_at not set on insert")
	}
	if got.ContentType.Valid || got.SizeBytes.Valid || got.LastModified.Valid {
		t.Errorf("metadata = %+v, want all NULL before indexing", got)
	}
}

func TestIndexStatLifecycle(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	fileID := insertTestFile(t, q, key)
	lm := time.Now().UTC().Truncate(time.Second)

	// Seed discovers the new file; seeding again must not duplicate it.
	for range 2 {
		if _, err := q.SeedIndexStat(ctx); err != nil {
			t.Fatalf("SeedIndexStat: %v", err)
		}
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "pending" || s.Attempts != 0 {
		t.Fatalf("after seed: %+v, want pending with 0 attempts", s)
	}

	// Claim: our row comes back with key and incremented attempts.
	ours := claimOurs(t, q, noStale, fileID)
	if len(ours) != 1 {
		t.Fatalf("claimed %v, want our row exactly once", ours)
	}
	if ours[0].Key != key || ours[0].Attempts != 1 {
		t.Errorf("claimed row = %+v, want key %q attempts 1", ours[0], key)
	}

	// A claimed row must not be claimable again (stale cutoff in the past).
	if again := claimOurs(t, q, noStale, fileID); len(again) != 0 {
		t.Errorf("re-claimed in-flight row: %v", again)
	}

	// Complete writes status and stat results atomically.
	if err := q.CompleteIndexStat(ctx, statResult(fileID, "text/plain", 42, lm)); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
	s := indexStatRow(t, conn, fileID)
	if s.Status != "done" || s.LastError.Valid {
		t.Fatalf("after complete: %+v, want done with no error", s)
	}
	if s.ContentType.String != "text/plain" || s.SizeBytes.Int64 != 42 || !s.LastModified.Time.Equal(lm) {
		t.Fatalf("results = %+v, want text/plain/42/%v", s, lm)
	}

	// The read model now serves the metadata.
	info, err := q.GetFile(ctx, fileID)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if info.ContentType.String != "text/plain" || info.SizeBytes.Int64 != 42 || !info.LastModified.Time.Equal(lm) {
		t.Errorf("file_infos = %+v, want stat results visible", info)
	}

	// Done rows are not claimable.
	if again := claimOurs(t, q, noStale, fileID); len(again) != 0 {
		t.Errorf("claimed a done row: %v", again)
	}

	// Fail with a future retry time: row is pending but not yet claimable.
	if _, err := q.ResetChangedIndexStat(ctx, ResetChangedIndexStatParams{
		Keys: []string{key}, Sizes: []int64{43}, LastModifieds: []pgtype.Timestamptz{ts(lm)},
	}); err != nil {
		t.Fatalf("ResetChangedIndexStat: %v", err)
	}
	ours = claimOurs(t, q, noStale, fileID)
	if len(ours) != 1 {
		t.Fatalf("claim after reset: got %v, want our row", ours)
	}
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := q.FailIndexStat(ctx, FailIndexStatParams{
		FileID: fileID, LastError: pgtype.Text{String: "stat boom", Valid: true},
		NextAttemptAt: ts(future), Exhausted: false,
	}); err != nil {
		t.Fatalf("FailIndexStat: %v", err)
	}
	s = indexStatRow(t, conn, fileID)
	if s.Status != "pending" || s.LastError.String != "stat boom" || !s.NextAttemptAt.Time.Equal(future) {
		t.Fatalf("after fail: %+v, want pending, error recorded, backoff applied", s)
	}
	if backedOff := claimOurs(t, q, noStale, fileID); len(backedOff) != 0 {
		t.Errorf("claimed a backed-off row before next_attempt_at: %v", backedOff)
	}

	// Exhausted failure parks the row as error; never claimable.
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
		t.Errorf("claimed an errored row: %v", parked)
	}
}

func TestResetChangedIndexStat(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	fileID := insertTestFile(t, q, key)
	lm := time.Now().UTC().Truncate(time.Second)
	indexTestFile(t, q, fileID, "text/plain", 42, lm)

	// Same listing as the stored results: nothing to do. Sub-second listing
	// precision must not trigger a false positive (results are stored
	// second-truncated).
	unchanged, err := q.ResetChangedIndexStat(ctx, ResetChangedIndexStatParams{
		Keys:          []string{key},
		Sizes:         []int64{42},
		LastModifieds: []pgtype.Timestamptz{ts(lm.Add(500 * time.Millisecond))},
	})
	if err != nil {
		t.Fatalf("ResetChangedIndexStat(unchanged): %v", err)
	}
	if unchanged != 0 {
		t.Errorf("reset %d rows for unchanged listing, want 0", unchanged)
	}

	// Changed size: the done row goes back to pending with attempts reset.
	changed, err := q.ResetChangedIndexStat(ctx, ResetChangedIndexStatParams{
		Keys:          []string{key},
		Sizes:         []int64{99},
		LastModifieds: []pgtype.Timestamptz{ts(lm)},
	})
	if err != nil {
		t.Fatalf("ResetChangedIndexStat(changed): %v", err)
	}
	if changed != 1 {
		t.Fatalf("reset %d rows for changed listing, want 1", changed)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "pending" || s.Attempts != 0 || s.LastError.Valid {
		t.Fatalf("after reset: %+v, want clean pending", s)
	}

	// Pending rows have no results to compare: a second reset is a no-op.
	again, err := q.ResetChangedIndexStat(ctx, ResetChangedIndexStatParams{
		Keys:          []string{key},
		Sizes:         []int64{99},
		LastModifieds: []pgtype.Timestamptz{ts(lm)},
	})
	if err != nil {
		t.Fatalf("ResetChangedIndexStat(pending): %v", err)
	}
	if again != 0 {
		t.Errorf("reset %d pending rows, want 0", again)
	}
}

func TestReleaseIndexStat(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	fileID := insertTestFile(t, q, key)
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}

	if ours := claimOurs(t, q, noStale, fileID); len(ours) != 1 {
		t.Fatalf("claim: got %v, want our row", ours)
	}

	// Release undoes the claim without consuming an attempt.
	if err := q.ReleaseIndexStat(ctx, fileID); err != nil {
		t.Fatalf("ReleaseIndexStat: %v", err)
	}
	s := indexStatRow(t, conn, fileID)
	if s.Status != "pending" || s.Attempts != 0 || s.ClaimedAt.Valid {
		t.Fatalf("after release: %+v, want pending, 0 attempts, no claim", s)
	}

	// Releasing a non-claimed row is a no-op (guard against clobbering a
	// row another worker already reclaimed and finished).
	if err := q.CompleteIndexStat(ctx, statResult(fileID, "text/plain", 1, time.Now().UTC())); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
	if err := q.ReleaseIndexStat(ctx, fileID); err != nil {
		t.Fatalf("ReleaseIndexStat(done row): %v", err)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "done" {
		t.Fatalf("release clobbered a done row: %+v", s)
	}
}

func TestClaimIndexStatReclaimsStale(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	fileID := insertTestFile(t, q, uniqueKey(t))
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}

	if ours := claimOurs(t, q, noStale, fileID); len(ours) != 1 {
		t.Fatalf("initial claim: got %v, want our row", ours)
	}

	// A cutoff in the future treats the fresh claim as expired (as if the
	// claiming worker died and the TTL elapsed): the row is claimable again
	// and attempts keep counting up.
	staleAll := ts(time.Now().UTC().Add(time.Hour))
	reclaimed := claimOurs(t, q, staleAll, fileID)
	if len(reclaimed) != 1 {
		t.Fatalf("stale reclaim: got %v, want our row", reclaimed)
	}
	if reclaimed[0].Attempts != 2 {
		t.Errorf("reclaimed attempts = %d, want 2", reclaimed[0].Attempts)
	}
}

func TestClaimIndexStatSkipLocked(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	// Two files pending; two concurrent transactions each claim a batch.
	// SKIP LOCKED must hand them disjoint rows without blocking.
	insertTestFile(t, q, uniqueKey(t)+"-a")
	insertTestFile(t, q, uniqueKey(t)+"-b")
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}

	// Claims happen on separate connections so their transactions overlap.
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
