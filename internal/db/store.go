package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps Queries with directory-index-maintaining variants of the
// files write queries (UpsertFiles, DeleteFile, DeleteFilesByIDs,
// DeleteUnseenFiles). Every place in this codebase that adds or removes
// files rows — the crawler and FilesService — depends on an interface
// (crawler.FileStore, files.FileIndex) that exposes only these wrapped
// methods, never the bare Queries ones, so it is a compile error for either
// to write files without also keeping directories in sync. See the
// directories table's doc comment in migrations/001_initial.sql for why
// that table exists and why it is maintained here rather than by a
// database trigger.
//
// Composition root wiring (cmd/*/main.go) constructs a *Store instead of
// calling New(pool) directly wherever files are written.
type Store struct {
	*Queries
	pool *pgxpool.Pool
}

// NewStore constructs a Store bound to pool. Queries methods promoted from
// the embedded *Queries run directly against the pool, same as New(pool);
// only the four WithDirectories methods below open their own transaction.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Queries: New(pool), pool: pool}
}

// withTx runs fn against tx-bound Queries in one transaction, committing on
// success; the deferred Rollback is a no-op once Commit has succeeded.
func (s *Store) withTx(ctx context.Context, fn func(q *Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpsertFilesWithDirectories runs UpsertFiles and UpsertDirectoriesForKeys
// in one transaction. Directory maintenance runs over the full arg.Keys
// regardless of which keys were actually new versus an existing key's
// marked_at merely being bumped — UpsertDirectoriesForKeys' ON CONFLICT DO
// NOTHING makes the redundant case (an existing key, whose ancestor
// directories necessarily already exist) a cheap no-op, and computing the
// true insert-only subset isn't worth complicating UpsertFiles' query for.
func (s *Store) UpsertFilesWithDirectories(ctx context.Context, arg UpsertFilesParams) (int64, error) {
	var n int64
	err := s.withTx(ctx, func(q *Queries) error {
		var err error
		if n, err = q.UpsertFiles(ctx, arg); err != nil {
			return err
		}
		return q.UpsertDirectoriesForKeys(ctx, arg.Keys)
	})
	return n, err
}

// DeleteFileWithDirectories runs DeleteFile and PruneDirectoriesForKeys in
// one transaction. Prune runs after the delete, in the same transaction, so
// its subtree-emptiness check observes post-delete state.
func (s *Store) DeleteFileWithDirectories(ctx context.Context, id int64) (File, error) {
	var f File
	err := s.withTx(ctx, func(q *Queries) error {
		var err error
		if f, err = q.DeleteFile(ctx, id); err != nil {
			return err
		}
		return q.PruneDirectoriesForKeys(ctx, []string{f.Key})
	})
	return f, err
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
	var n int64
	err := s.withTx(ctx, func(q *Queries) error {
		var err error
		if n, err = q.DeleteFilesByIDs(ctx, ids); err != nil {
			return err
		}
		return q.PruneDirectoriesForKeys(ctx, keys)
	})
	return n, err
}

// DeleteUnseenFilesWithDirectories runs DeleteUnseenFiles and
// PruneOrphanDirectories in one transaction: the crawler's out-of-band S3
// delete reconciliation (see crawler.FileStore's doc comment). cutoff must
// come from DatabaseNow, read before the crawl's walk started — see
// DeleteUnseenFiles' doc comment for why.
//
// Directory pruning cannot be folded into DeleteUnseenFiles as a single
// statement (e.g. a data-modifying CTE): every sub-statement of one SQL
// statement runs against the same snapshot, so a prune driven off files as
// it stood before this delete would see every swept directory as still
// occupied and remove nothing. It must be a second statement, in the same
// transaction, after the delete — same shape as DeleteFileWithDirectories
// and DeleteFilesByIDsWithDirectories above.
//
// PruneOrphanDirectories (unscoped) rather than collecting the swept keys
// via DeleteUnseenFiles :many and calling the keys-scoped
// PruneDirectoriesForKeys: an out-of-band deletion has no natural bound on
// how many rows it sweeps (a wrong bucket, or a large prefix deleted
// directly in S3, could be the whole table), and streaming that many keys
// through the crawler process is worse than one unscoped pass over
// directories — a table sized by directory count, not file count, and
// already measured cheap at 2000+ candidates (see PruneOrphanDirectories'
// doc comment in queries/directories.sql).
func (s *Store) DeleteUnseenFilesWithDirectories(ctx context.Context, cutoff time.Time) (int64, error) {
	var n int64
	err := s.withTx(ctx, func(q *Queries) error {
		var err error
		if n, err = q.DeleteUnseenFiles(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true}); err != nil {
			return err
		}
		return q.PruneOrphanDirectories(ctx)
	})
	return n, err
}
