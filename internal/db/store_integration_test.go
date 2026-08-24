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
// three WithDirectories methods: a closed pool makes Begin fail immediately,
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


