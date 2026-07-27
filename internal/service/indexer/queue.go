package indexer

import (
	"context"
	"file-indexer/internal/db"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Queries is the slice of db.Queries PGQueue depends on outside of the
// completion transaction (Complete opens its own via the pool, since it
// must run the status flip and the result store together).
type Queries interface {
	SeedIndexQueue(ctx context.Context, indexType string) (int64, error)
	RequeueStaleIndexQueue(ctx context.Context, indexType string) (int64, error)
	ClaimIndexQueue(ctx context.Context, arg db.ClaimIndexQueueParams) ([]db.ClaimIndexQueueRow, error)
	FailIndexQueue(ctx context.Context, arg db.FailIndexQueueParams) error
}

// StoreFunc persists one index type's result. Complete calls it in the same
// transaction as the queue's done-status write, so a job is never marked
// done without its result committed, or vice versa.
type StoreFunc[R any] func(ctx context.Context, q *db.Queries, fileID int64, result R) error

// PGQueue adapts the shared index_queue table (plus one index type's
// StoreFunc) to the Queue interface. It is the only Queue implementation:
// adding an index type means providing a ProcessFunc and a StoreFunc, not a
// new Queue.
type PGQueue[R any] struct {
	pool      *pgxpool.Pool
	queries   Queries
	indexType string
	store     StoreFunc[R]
}

var (
	_ Queue[StatResult]       = (*PGQueue[StatResult])(nil)
	_ ProcessFunc[StatResult] = (*StatIndexer)(nil).Process
	_ StoreFunc[StatResult]   = StoreStatResult

	_ Queue[PreviewResult]       = (*PGQueue[PreviewResult])(nil)
	_ ProcessFunc[PreviewResult] = (*PreviewIndexer)(nil).Process
	_ StoreFunc[PreviewResult]   = StorePreviewResult
)

// NewPGQueue constructs a PGQueue for one index type.
func NewPGQueue[R any](pool *pgxpool.Pool, indexType string, store StoreFunc[R]) *PGQueue[R] {
	return &PGQueue[R]{
		pool:      pool,
		queries:   db.New(pool),
		indexType: indexType,
		store:     store,
	}
}

func (q *PGQueue[R]) Seed(ctx context.Context) (int64, error) {
	seeded, err := q.queries.SeedIndexQueue(ctx, q.indexType)
	if err != nil {
		return 0, err
	}
	requeued, err := q.queries.RequeueStaleIndexQueue(ctx, q.indexType)
	if err != nil {
		return 0, err
	}
	return seeded + requeued, nil
}

func (q *PGQueue[R]) Claim(ctx context.Context, limit int32, staleTimeout time.Duration) ([]Job, error) {
	rows, err := q.queries.ClaimIndexQueue(ctx, db.ClaimIndexQueueParams{
		IndexType:    q.indexType,
		StaleTimeout: pgtype.Interval{Microseconds: staleTimeout.Microseconds(), Valid: true},
		BatchSize:    limit,
	})
	if err != nil {
		return nil, err
	}

	jobs := make([]Job, len(rows))
	for i, row := range rows {
		jobs[i] = Job{FileID: row.FileID, Key: row.Key, Attempts: row.Attempts}
	}
	return jobs, nil
}

// Complete runs the queue status flip and the result store in one
// transaction.
func (q *PGQueue[R]) Complete(ctx context.Context, fileID int64, result R) error {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once Commit has succeeded

	txQueries := db.New(tx)
	if err := txQueries.CompleteIndexQueue(ctx, db.CompleteIndexQueueParams{
		IndexType: q.indexType,
		FileID:    fileID,
	}); err != nil {
		return err
	}
	if err := q.store(ctx, txQueries, fileID, result); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (q *PGQueue[R]) Fail(ctx context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error {
	return q.queries.FailIndexQueue(ctx, db.FailIndexQueueParams{
		IndexType:     q.indexType,
		FileID:        fileID,
		LastError:     pgtype.Text{String: cause, Valid: true},
		NextAttemptAt: pgtype.Timestamptz{Time: nextAttempt, Valid: true},
		Exhausted:     exhausted,
	})
}
