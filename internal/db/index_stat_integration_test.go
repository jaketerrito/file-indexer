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

// upsertTestFiles registers files via the crawler ingest path and returns
// the new/changed ids. Cleanup deletes the file rows; index_stat rows go
// with them via ON DELETE CASCADE.
func upsertTestFiles(t *testing.T, q *Queries, arg UpsertFilesParams) []int64 {
	t.Helper()
	ids, err := q.UpsertFiles(context.Background(), arg)
	if err != nil {
		t.Fatalf("UpsertFiles: %v", err)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = q.DeleteFile(context.Background(), id)
		}
	})
	return ids
}

// indexStatRow reads a row directly; the production queries deliberately
// expose no point read, but tests need to observe state transitions.
func indexStatRow(t *testing.T, conn *pgx.Conn, fileID int64) IndexStat {
	t.Helper()
	var s IndexStat
	err := conn.QueryRow(context.Background(),
		`SELECT file_id, status, attempts, next_attempt_at, claimed_at, last_error, updated_at
		 FROM index_stat WHERE file_id = $1`, fileID).
		Scan(&s.FileID, &s.Status, &s.Attempts, &s.NextAttemptAt, &s.ClaimedAt, &s.LastError, &s.UpdatedAt)
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

func TestUpsertFilesChangeDetection(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	keyA, keyB := uniqueKey(t)+"-a", uniqueKey(t)+"-b"
	// UpsertFiles stores timestamps truncated to whole seconds (see the
	// query comment); use second precision so equality assertions hold.
	lm := time.Now().UTC().Truncate(time.Second)

	// New files: both ids returned.
	ids := upsertTestFiles(t, q, UpsertFilesParams{
		Keys:          []string{keyA, keyB},
		Sizes:         []int64{1, 2},
		LastModifieds: []pgtype.Timestamptz{ts(lm), ts(lm)},
	})
	if len(ids) != 2 {
		t.Fatalf("UpsertFiles(new) returned %d ids, want 2", len(ids))
	}

	// Same listing again: nothing changed, nothing returned.
	same, err := q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:          []string{keyA, keyB},
		Sizes:         []int64{1, 2},
		LastModifieds: []pgtype.Timestamptz{ts(lm), ts(lm)},
	})
	if err != nil {
		t.Fatalf("UpsertFiles(unchanged): %v", err)
	}
	if len(same) != 0 {
		t.Errorf("UpsertFiles(unchanged) returned %v, want none", same)
	}

	// One file changed size: only its id returned, row updated.
	lm2 := lm.Add(time.Hour)
	changed, err := q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:          []string{keyA, keyB},
		Sizes:         []int64{99, 2},
		LastModifieds: []pgtype.Timestamptz{ts(lm2), ts(lm)},
	})
	if err != nil {
		t.Fatalf("UpsertFiles(changed): %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("UpsertFiles(changed) returned %v, want exactly the changed id", changed)
	}

	got, err := q.GetFile(ctx, changed[0])
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if got.Key != keyA || got.SizeBytes.Int64 != 99 || !got.UpdatedAt.Time.Equal(lm2) {
		t.Errorf("changed file = %+v, want key %q size 99 updated_at %v", got, keyA, lm2)
	}
	if !got.CreatedAt.Time.Equal(lm) {
		t.Errorf("created_at = %v, want original %v (must not change on update)", got.CreatedAt.Time, lm)
	}
}

func TestIndexStatLifecycle(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	lm := time.Now().UTC().Truncate(time.Microsecond)
	ids := upsertTestFiles(t, q, UpsertFilesParams{
		Keys: []string{key}, Sizes: []int64{1}, LastModifieds: []pgtype.Timestamptz{ts(lm)},
	})
	fileID := ids[0]

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

	// Complete: done, error cleared.
	if err := q.CompleteIndexStat(ctx, fileID); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "done" || s.LastError.Valid {
		t.Fatalf("after complete: %+v, want done with no error", s)
	}

	// Done rows are not claimable.
	if again := claimOurs(t, q, noStale, fileID); len(again) != 0 {
		t.Errorf("claimed a done row: %v", again)
	}

	// Reset (crawler saw the object change): pending again, attempts reset.
	n, err := q.ResetIndexStat(ctx, []int64{fileID})
	if err != nil {
		t.Fatalf("ResetIndexStat: %v", err)
	}
	if n != 1 {
		t.Fatalf("ResetIndexStat reset %d rows, want 1", n)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "pending" || s.Attempts != 0 {
		t.Fatalf("after reset: %+v, want pending with 0 attempts", s)
	}

	// Fail with a future retry time: row is pending but not yet claimable.
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
	s := indexStatRow(t, conn, fileID)
	if s.Status != "pending" || s.LastError.String != "stat boom" || !s.NextAttemptAt.Time.Equal(future) {
		t.Fatalf("after fail: %+v, want pending, error recorded, backoff applied", s)
	}
	if backedOff := claimOurs(t, q, noStale, fileID); len(backedOff) != 0 {
		t.Errorf("claimed a backed-off row before next_attempt_at: %v", backedOff)
	}

	// Reset must also cover pending-with-backoff rows (the crawler saw the
	// object change mid-retry): failure state clears and the row is
	// claimable immediately instead of inheriting the old backoff.
	n, err = q.ResetIndexStat(ctx, []int64{fileID})
	if err != nil {
		t.Fatalf("ResetIndexStat(backed-off): %v", err)
	}
	if n != 1 {
		t.Fatalf("ResetIndexStat(backed-off) reset %d rows, want 1", n)
	}
	if s := indexStatRow(t, conn, fileID); s.Status != "pending" || s.Attempts != 0 || s.LastError.Valid {
		t.Fatalf("after reset of backed-off row: %+v, want clean pending", s)
	}
	if ours = claimOurs(t, q, noStale, fileID); len(ours) != 1 {
		t.Fatalf("claim after backed-off reset: got %v, want our row", ours)
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

func TestReleaseIndexStat(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	ids := upsertTestFiles(t, q, UpsertFilesParams{
		Keys: []string{key}, Sizes: []int64{1},
		LastModifieds: []pgtype.Timestamptz{ts(time.Now().UTC().Truncate(time.Second))},
	})
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}

	if ours := claimOurs(t, q, noStale, ids[0]); len(ours) != 1 {
		t.Fatalf("claim: got %v, want our row", ours)
	}

	// Release undoes the claim without consuming an attempt.
	if err := q.ReleaseIndexStat(ctx, ids[0]); err != nil {
		t.Fatalf("ReleaseIndexStat: %v", err)
	}
	s := indexStatRow(t, conn, ids[0])
	if s.Status != "pending" || s.Attempts != 0 || s.ClaimedAt.Valid {
		t.Fatalf("after release: %+v, want pending, 0 attempts, no claim", s)
	}

	// Releasing a non-claimed row is a no-op (guard against clobbering a
	// row another worker already reclaimed and finished).
	if err := q.CompleteIndexStat(ctx, ids[0]); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
	if err := q.ReleaseIndexStat(ctx, ids[0]); err != nil {
		t.Fatalf("ReleaseIndexStat(done row): %v", err)
	}
	if s := indexStatRow(t, conn, ids[0]); s.Status != "done" {
		t.Fatalf("release clobbered a done row: %+v", s)
	}
}

func TestClaimIndexStatReclaimsStale(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	ids := upsertTestFiles(t, q, UpsertFilesParams{
		Keys: []string{key}, Sizes: []int64{1},
		LastModifieds: []pgtype.Timestamptz{ts(time.Now().UTC().Truncate(time.Microsecond))},
	})
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}

	if ours := claimOurs(t, q, noStale, ids[0]); len(ours) != 1 {
		t.Fatalf("initial claim: got %v, want our row", ours)
	}

	// A cutoff in the future treats the fresh claim as expired (as if the
	// claiming worker died and the TTL elapsed): the row is claimable again
	// and attempts keep counting up.
	staleAll := ts(time.Now().UTC().Add(time.Hour))
	reclaimed := claimOurs(t, q, staleAll, ids[0])
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
	keyA, keyB := uniqueKey(t)+"-a", uniqueKey(t)+"-b"
	lm := ts(time.Now().UTC().Truncate(time.Microsecond))
	upsertTestFiles(t, q, UpsertFilesParams{
		Keys: []string{keyA, keyB}, Sizes: []int64{1, 2},
		LastModifieds: []pgtype.Timestamptz{lm, lm},
	})
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
