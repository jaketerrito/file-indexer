//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestStoreBeginErrors exercises the pool.Begin failure branch shared by all
// four WithDirectories methods: a closed pool makes Begin fail immediately,
// before any query runs.
func TestStoreBeginErrors(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	pool.Close()

	if _, err := store.UpsertFilesWithDirectories(ctx, UpsertFilesParams{
		Keys:      []string{"a"},
		MarkedAts: []pgtype.Timestamptz{{Time: time.Now().UTC(), Valid: true}},
	}); err == nil {
		t.Error("UpsertFilesWithDirectories on a closed pool: want error, got nil")
	}
	if _, err := store.DeleteFileWithDirectories(ctx, 1); err == nil {
		t.Error("DeleteFileWithDirectories on a closed pool: want error, got nil")
	}
	if _, err := store.DeleteFilesByIDsWithDirectories(ctx, []int64{1}, []string{"a"}); err == nil {
		t.Error("DeleteFilesByIDsWithDirectories on a closed pool: want error, got nil")
	}
	if _, err := store.DeleteUnseenFilesWithDirectories(ctx, time.Now().UTC()); err == nil {
		t.Error("DeleteUnseenFilesWithDirectories on a closed pool: want error, got nil")
	}
}

// TestUpsertFilesWithDirectoriesRollsBackOnFailure confirms both the
// error propagates and the transaction actually rolled back: a marked_at
// left invalid (parallel unnest of mismatched-length Keys/MarkedAts pads
// the shorter array with NULL) violates files.marked_at's NOT NULL
// constraint, so neither files row is left committed — not even the one
// whose own marked_at was valid.
func TestUpsertFilesWithDirectoriesRollsBackOnFailure(t *testing.T) {
	pool := testPool(t)
	conn := testConn(t)
	store := NewStore(pool)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	_, err := store.UpsertFilesWithDirectories(ctx, UpsertFilesParams{
		Keys:      []string{prefix + "a.txt", prefix + "b.txt"},
		MarkedAts: []pgtype.Timestamptz{{Time: time.Now().UTC(), Valid: true}}, // one short: b.txt's pads to NULL
	})
	if err == nil {
		t.Fatal("expected a NOT NULL violation on marked_at")
	}

	var count int
	if scanErr := conn.QueryRow(ctx, `SELECT count(*) FROM files WHERE key LIKE $1`, prefix+"%").Scan(&count); scanErr != nil {
		t.Fatalf("count files: %v", scanErr)
	}
	if count != 0 {
		t.Errorf("files rows after failed upsert = %d, want 0 (transaction should have rolled back)", count)
	}

	children, err := store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("directories after failed upsert = %v, want none", children)
	}
}

// TestDeleteFileWithDirectoriesNotFound confirms DeleteFileWithDirectories
// surfaces pgx.ErrNoRows (via DeleteFile's :one RETURNING) for a
// nonexistent id, and never reaches PruneDirectoriesForKeys.
func TestDeleteFileWithDirectoriesNotFound(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.DeleteFileWithDirectories(ctx, -1)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("error = %v, want pgx.ErrNoRows", err)
	}
}

// backdateSeenAt directly sets a files row's seen_at, simulating "this row
// was last confirmed present by a crawl a while ago" — no production code
// path sets seen_at to anything but now(), so tests that need a stale row
// must reach past UpsertFiles to set it up.
func backdateSeenAt(t *testing.T, conn *pgx.Conn, id int64, seenAt time.Time) {
	t.Helper()
	if _, err := conn.Exec(context.Background(),
		`UPDATE files SET seen_at = $1 WHERE id = $2`, seenAt, id); err != nil {
		t.Fatalf("backdate seen_at: %v", err)
	}
}

// TestUpsertFilesStampsSeenAt confirms UpsertFiles refreshes seen_at on
// every re-upsert, not just on insert (the column DEFAULT covers insert;
// this is the ON CONFLICT branch DeleteUnseenFiles depends on for liveness
// tracking — see that query's doc comment).
func TestUpsertFilesStampsSeenAt(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	key := uniqueKey(t)

	id := insertTestFile(t, conn, key)
	backdateSeenAt(t, conn, id, time.Now().UTC().Add(-time.Hour))

	if _, err := q.UpsertFiles(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{{Time: time.Now().UTC(), Valid: true}},
	}); err != nil {
		t.Fatalf("UpsertFiles (re-upsert): %v", err)
	}

	var seenAt pgtype.Timestamptz
	if err := conn.QueryRow(ctx, `SELECT seen_at FROM files WHERE id = $1`, id).Scan(&seenAt); err != nil {
		t.Fatalf("scan seen_at: %v", err)
	}
	if time.Since(seenAt.Time) > time.Minute {
		t.Errorf("seen_at = %v, want refreshed to ~now by the re-upsert", seenAt.Time)
	}
}

// TestDatabaseNow confirms DatabaseNow returns the database's current time,
// not a zero value or something derived from the test process's clock.
func TestDatabaseNow(t *testing.T) {
	conn := testConn(t)
	q := New(conn)

	got, err := q.DatabaseNow(context.Background())
	if err != nil {
		t.Fatalf("DatabaseNow: %v", err)
	}
	if !got.Valid {
		t.Fatal("DatabaseNow returned an invalid timestamp")
	}
	if d := time.Since(got.Time); d < 0 || d > time.Minute {
		t.Errorf("DatabaseNow = %v, want within the last minute of %v", got.Time, time.Now().UTC())
	}
}

// TestDeleteUnseenFilesDeletesOnlyStaleRows confirms the cutoff boundary is
// exactly what DeleteUnseenFiles' doc comment claims: strictly-older rows
// go, rows at-or-after cutoff survive.
func TestDeleteUnseenFilesDeletesOnlyStaleRows(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	staleID := insertTestFile(t, conn, uniqueKey(t)+"-stale")
	freshID := insertTestFile(t, conn, uniqueKey(t)+"-fresh")

	old := time.Now().UTC().Add(-time.Hour)
	backdateSeenAt(t, conn, staleID, old)

	cutoff := time.Now().UTC()
	// freshID keeps its just-inserted seen_at (from insertTestFile, before
	// cutoff was read). To isolate the boundary being tested (stale vs.
	// fresh relative to a fixed cutoff) rather than insertion-order timing,
	// re-stamp it to strictly after cutoff — mirroring how a real crawl
	// re-stamps every object it still sees during its walk.
	if _, err := conn.Exec(ctx, `UPDATE files SET seen_at = now() WHERE id = $1`, freshID); err != nil {
		t.Fatalf("re-stamp fresh file: %v", err)
	}

	deleted, err := q.DeleteUnseenFiles(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		t.Fatalf("DeleteUnseenFiles: %v", err)
	}
	if deleted != 1 {
		t.Errorf("DeleteUnseenFiles deleted %d rows, want 1 (only the stale one)", deleted)
	}

	if _, err := q.GetFile(ctx, staleID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("stale file after sweep: err = %v, want pgx.ErrNoRows", err)
	}
	if _, err := q.GetFile(ctx, freshID); err != nil {
		t.Errorf("fresh file after sweep: unexpected error %v, want it to survive", err)
	}
}

// TestDeleteUnseenFilesWithDirectoriesPrunesDirectories confirms the sweep
// is atomic with directory pruning: a swept file's now-empty ancestor
// directory disappears in the same call, not just the files row.
func TestDeleteUnseenFilesWithDirectoriesPrunesDirectories(t *testing.T) {
	pool := testPool(t)
	conn := testConn(t)
	store := NewStore(pool)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"
	key := prefix + "sub/leaf.txt"

	id, err := store.UpsertFilesWithDirectories(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{{Time: time.Now().UTC(), Valid: true}},
	})
	if err != nil {
		t.Fatalf("UpsertFilesWithDirectories: %v", err)
	}
	if id != 1 {
		t.Fatalf("UpsertFilesWithDirectories rows affected = %d, want 1", id)
	}

	var fileID int64
	if err := conn.QueryRow(ctx, `SELECT id FROM files WHERE key = $1`, key).Scan(&fileID); err != nil {
		t.Fatalf("lookup file id: %v", err)
	}
	backdateSeenAt(t, conn, fileID, time.Now().UTC().Add(-time.Hour))

	children, err := store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories (before sweep): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"sub/" {
		t.Fatalf("children before sweep = %v, want [%ssub/]", children, prefix)
	}

	deleted, err := store.DeleteUnseenFilesWithDirectories(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("DeleteUnseenFilesWithDirectories: %v", err)
	}
	if deleted != 1 {
		t.Errorf("DeleteUnseenFilesWithDirectories deleted %d rows, want 1", deleted)
	}

	if _, err := store.GetFile(ctx, fileID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetFile after sweep: err = %v, want pgx.ErrNoRows", err)
	}
	children, err = store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories (after sweep): %v", err)
	}
	if len(children) != 0 {
		t.Errorf("children after sweep = %v, want none (sub/ orphaned by the swept file)", children)
	}
}
