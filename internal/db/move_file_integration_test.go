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

// TestMoveFileWithDirectoriesMovesKeyAndMaintainsDirectories moves a file
// between directories and confirms the key changes, the old directory tree
// is pruned when empty, and the new directory tree is created.
func TestMoveFileWithDirectoriesMovesKeyAndMaintainsDirectories(t *testing.T) {
	pool := testPool(t)
	conn := testConn(t)
	store := NewStore(pool)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"
	// Tests that seed directly through Store do not get insertTestFile's
	// per-row cleanup, so sweep the prefix explicitly to avoid leaking stale
	// rows into later DeleteUnseenFiles tests.
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `DELETE FROM files WHERE key LIKE $1`, prefix+"%")
	})

	// Seed through the store wrapper so directories are created.
	oldKey := prefix + "a/file.txt"
	otherKey := prefix + "b/other.txt"
	now := pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true}
	if _, err := store.UpsertFilesWithDirectories(ctx, UpsertFilesParams{
		Keys:      []string{oldKey, otherKey},
		MarkedAts: []pgtype.Timestamptz{now, now},
	}); err != nil {
		t.Fatalf("UpsertFilesWithDirectories: %v", err)
	}
	var id int64
	var originalMarkedAt time.Time
	if err := conn.QueryRow(ctx, `SELECT id, marked_at FROM files WHERE key = $1`, oldKey).Scan(&id, &originalMarkedAt); err != nil {
		t.Fatalf("lookup source id: %v", err)
	}

	newKey := prefix + "b/file.txt"
	moved, err := store.MoveFileWithDirectories(ctx, MoveFileWithDirectoriesParams{
		ID:     id,
		NewKey: newKey,
	})
	if err != nil {
		t.Fatalf("MoveFileWithDirectories: %v", err)
	}
	if moved.Key != newKey {
		t.Errorf("moved.Key = %q, want %q", moved.Key, newKey)
	}

	got, err := store.GetFileByKey(ctx, newKey)
	if err != nil {
		t.Fatalf("GetFileByKey(newKey): %v", err)
	}
	if got.ID != id {
		t.Errorf("GetFileByKey id = %d, want %d", got.ID, id)
	}

	// A move is a pure path change; marked_at must NOT be bumped so the
	// index queue staleness rule does not re-enqueue stat/preview/exif work.
	var movedMarkedAt time.Time
	if err := conn.QueryRow(ctx, `SELECT marked_at FROM files WHERE id = $1`, id).Scan(&movedMarkedAt); err != nil {
		t.Fatalf("lookup moved marked_at: %v", err)
	}
	if !movedMarkedAt.Equal(originalMarkedAt) {
		t.Errorf("marked_at changed: got %v, want %v", movedMarkedAt, originalMarkedAt)
	}

	if _, err := store.GetFileByKey(ctx, oldKey); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetFileByKey(oldKey) err = %v, want pgx.ErrNoRows", err)
	}

	// a/ should be pruned because its only file moved away; b/ survives.
	children, err := store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"b/" {
		t.Errorf("children = %v, want [%sb/]", children, prefix)
	}
}

// TestMoveFileWithDirectoriesCreatesNewFolderTree moves a file to a
// previously-unseen directory and confirms every ancestor is created.
func TestMoveFileWithDirectoriesCreatesNewFolderTree(t *testing.T) {
	pool := testPool(t)
	conn := testConn(t)
	store := NewStore(pool)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	oldKey := prefix + "src/file.txt"
	id := insertTestFile(t, conn, oldKey)

	newKey := prefix + "x/y/z/file.txt"
	if _, err := store.MoveFileWithDirectories(ctx, MoveFileWithDirectoriesParams{
		ID:     id,
		NewKey: newKey,
	}); err != nil {
		t.Fatalf("MoveFileWithDirectories: %v", err)
	}

	children, err := store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(root): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"x/" {
		t.Fatalf("children = %v, want [%sx/]", children, prefix)
	}

	children, err = store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix + "x/", PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(x/): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"x/y/" {
		t.Fatalf("children of x/ = %v, want [%sx/y/]", children, prefix)
	}

	children, err = store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix + "x/y/", PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(x/y/): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"x/y/z/" {
		t.Fatalf("children of x/y/ = %v, want [%sx/y/z/]", children, prefix)
	}
}

// TestMoveFileWithDirectoriesRenameWithinSameFolder confirms that a key
// change within the same directory (a rename) works and leaves the parent
// directory intact.
func TestMoveFileWithDirectoriesRenameWithinSameFolder(t *testing.T) {
	pool := testPool(t)
	conn := testConn(t)
	store := NewStore(pool)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	oldKey := prefix + "dir/old.txt"
	id := insertTestFile(t, conn, oldKey)

	newKey := prefix + "dir/new.txt"
	if _, err := store.MoveFileWithDirectories(ctx, MoveFileWithDirectoriesParams{
		ID:     id,
		NewKey: newKey,
	}); err != nil {
		t.Fatalf("MoveFileWithDirectories: %v", err)
	}

	got, err := store.GetFileByKey(ctx, newKey)
	if err != nil {
		t.Fatalf("GetFileByKey: %v", err)
	}
	if got.ID != id {
		t.Errorf("id = %d, want %d", got.ID, id)
	}

	if _, err := store.GetFileByKey(ctx, oldKey); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetFileByKey(oldKey) err = %v, want pgx.ErrNoRows", err)
	}

	children, err := store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"dir/" {
		t.Errorf("children = %v, want [%sdir/]", children, prefix)
	}
}

// TestMoveFileWithDirectoriesUniqueViolation confirms that moving a file
// onto an existing key fails with a unique-key error and the transaction
// rolls back (source row and directories unchanged).
func TestMoveFileWithDirectoriesUniqueViolation(t *testing.T) {
	pool := testPool(t)
	conn := testConn(t)
	store := NewStore(pool)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"
	// Seed through Store, so clean up the prefix explicitly to avoid leaking
	// stale rows into later DeleteUnseenFiles tests.
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `DELETE FROM files WHERE key LIKE $1`, prefix+"%")
	})

	oldKey := prefix + "a/file.txt"
	existingKey := prefix + "b/existing.txt"

	// Seed through the store wrapper so directories are created and survive
	// the rollback we are about to trigger.
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	if _, err := store.UpsertFilesWithDirectories(ctx, UpsertFilesParams{
		Keys:      []string{oldKey, existingKey},
		MarkedAts: []pgtype.Timestamptz{now, now},
	}); err != nil {
		t.Fatalf("UpsertFilesWithDirectories: %v", err)
	}
	var id int64
	if err := conn.QueryRow(ctx, `SELECT id FROM files WHERE key = $1`, oldKey).Scan(&id); err != nil {
		t.Fatalf("lookup source id: %v", err)
	}

	_, err := store.MoveFileWithDirectories(ctx, MoveFileWithDirectoriesParams{
		ID:     id,
		NewKey: existingKey,
	})
	if err == nil {
		t.Fatal("MoveFileWithDirectories onto existing key: want error, got nil")
	}

	// Source row must survive the rollback.
	got, err := store.GetFileByKey(ctx, oldKey)
	if err != nil {
		t.Fatalf("GetFileByKey(oldKey): %v", err)
	}
	if got.ID != id {
		t.Errorf("source id = %d, want %d", got.ID, id)
	}

	// Both source and destination directories must survive the rollback.
	want := map[string]bool{prefix + "a/": true, prefix + "b/": true}
	children, err := store.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	gotDirs := make(map[string]bool, len(children))
	for _, c := range children {
		gotDirs[c] = true
	}
	if len(gotDirs) != len(want) {
		t.Errorf("children = %v, want %v", children, want)
	}
	for w := range want {
		if !gotDirs[w] {
			t.Errorf("missing directory %q", w)
		}
	}
}

// TestMoveFileWithDirectoriesNotFound confirms that moving a nonexistent
// id surfaces pgx.ErrNoRows and never touches directories.
func TestMoveFileWithDirectoriesNotFound(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.MoveFileWithDirectories(ctx, MoveFileWithDirectoriesParams{
		ID:     -1,
		NewKey: "does/not/matter.txt",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("error = %v, want pgx.ErrNoRows", err)
	}
}
