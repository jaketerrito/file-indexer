package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps Queries with directory-index-maintaining variants of the
// files write queries (UpsertFiles, DeleteFile, DeleteFilesByIDs). Every
// place in this codebase that adds or removes files rows — the crawler and
// FilesService — depends on an interface (crawler.FileStore,
// files.FileIndex) that exposes only these wrapped methods, never the bare
// Queries ones, so it is a compile error for either to write files without
// also keeping directories in sync. See the directories table's doc
// comment in migrations/001_initial.sql for why that table exists and why
// it is maintained here rather than by a database trigger.
//
// Composition root wiring (cmd/*/main.go) constructs a *Store instead of
// calling New(pool) directly wherever files are written.
type Store struct {
	*Queries
	pool *pgxpool.Pool
}

// NewStore constructs a Store bound to pool. Queries methods promoted from
// the embedded *Queries run directly against the pool, same as New(pool);
// only the three WithDirectories methods below open their own transaction.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Queries: New(pool), pool: pool}
}

// UpsertFilesWithDirectories runs UpsertFiles and UpsertDirectoriesForKeys
// in one transaction. Directory maintenance runs over the full arg.Keys
// regardless of which keys were actually new versus an existing key's
// marked_at merely being bumped — UpsertDirectoriesForKeys' ON CONFLICT DO
// NOTHING makes the redundant case (an existing key, whose ancestor
// directories necessarily already exist) a cheap no-op, and computing the
// true insert-only subset isn't worth complicating UpsertFiles' query for.
func (s *Store) UpsertFilesWithDirectories(ctx context.Context, arg UpsertFilesParams) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once Commit has succeeded

	txQueries := New(tx)
	n, err := txQueries.UpsertFiles(ctx, arg)
	if err != nil {
		return 0, err
	}
	if err := txQueries.UpsertDirectoriesForKeys(ctx, arg.Keys); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// DeleteFileWithDirectories runs DeleteFile and PruneDirectoriesForKeys in
// one transaction. Prune runs after the delete, in the same transaction, so
// its subtree-emptiness check observes post-delete state.
func (s *Store) DeleteFileWithDirectories(ctx context.Context, id int64) (File, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txQueries := New(tx)
	f, err := txQueries.DeleteFile(ctx, id)
	if err != nil {
		return File{}, err
	}
	if err := txQueries.PruneDirectoriesForKeys(ctx, []string{f.Key}); err != nil {
		return File{}, err
	}
	return f, tx.Commit(ctx)
}

// DeleteFilesByIDsWithDirectories runs DeleteFilesByIDs and
// PruneDirectoriesForKeys in one transaction, over keys the caller already
// has (DeleteDirectory gets them from ListFilesForDelete, so this never
// needs to re-fetch). This makes one DeleteDirectory batch atomic;
// DeleteDirectory's own cross-batch non-atomicity (see
// DeleteDirectoryResponse's doc comment) is unaffected by design — a
// failure partway through a directory delete still leaves a prefix of
// files+directories gone and the rest untouched, and retrying with the
// same path continues where it left off.
func (s *Store) DeleteFilesByIDsWithDirectories(ctx context.Context, ids []int64, keys []string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txQueries := New(tx)
	n, err := txQueries.DeleteFilesByIDs(ctx, ids)
	if err != nil {
		return 0, err
	}
	if err := txQueries.PruneDirectoriesForKeys(ctx, keys); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}
