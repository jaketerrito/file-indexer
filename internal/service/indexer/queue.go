package indexer

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/worker"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// StatQueries is the slice of db.Queries the stat queue adapter depends on.
type StatQueries interface {
	SeedIndexStat(ctx context.Context) (int64, error)
	RequeueStaleIndexStat(ctx context.Context) (int64, error)
	ClaimIndexStat(ctx context.Context, arg db.ClaimIndexStatParams) ([]db.ClaimIndexStatRow, error)
	ReleaseIndexStat(ctx context.Context, fileID int64) error
	CompleteIndexStat(ctx context.Context, arg db.CompleteIndexStatParams) error
	FailIndexStat(ctx context.Context, arg db.FailIndexStatParams) error
}

// StatQueue adapts the index_stat sqlc queries to the worker.Queue
// interface. It holds no state beyond the query handle; all timing and retry
// policy comes from the worker pool.
type StatQueue struct {
	queries StatQueries
}

// NewStatQueue constructs a StatQueue.
func NewStatQueue(queries StatQueries) *StatQueue {
	return &StatQueue{queries: queries}
}

var _ worker.Queue[StatResult] = (*StatQueue)(nil)

// Seed discovers new files (no index_stat row yet) and re-enqueues done
// rows whose stored mark no longer matches files.marked_at (edited objects).
// Both are run together so the pool's single SeedInterval covers both cases.
func (q *StatQueue) Seed(ctx context.Context) (int64, error) {
	seeded, err := q.queries.SeedIndexStat(ctx)
	if err != nil {
		return 0, err
	}
	requeued, err := q.queries.RequeueStaleIndexStat(ctx)
	if err != nil {
		return 0, err
	}
	return seeded + requeued, nil
}

func (q *StatQueue) Claim(ctx context.Context, limit int32, staleBefore time.Time) ([]worker.Job, error) {
	rows, err := q.queries.ClaimIndexStat(ctx, db.ClaimIndexStatParams{
		StaleBefore: pgtype.Timestamptz{Time: staleBefore, Valid: true},
		BatchSize:   limit,
	})
	if err != nil {
		return nil, err
	}

	jobs := make([]worker.Job, len(rows))
	for i, row := range rows {
		jobs[i] = worker.Job{FileID: row.FileID, Key: row.Key, Attempts: row.Attempts}
	}
	return jobs, nil
}

func (q *StatQueue) Release(ctx context.Context, fileID int64) error {
	return q.queries.ReleaseIndexStat(ctx, fileID)
}

func (q *StatQueue) Complete(ctx context.Context, fileID int64, result StatResult) error {
	return q.queries.CompleteIndexStat(ctx, db.CompleteIndexStatParams{
		FileID:       fileID,
		ContentType:  pgtype.Text{String: result.ContentType, Valid: true},
		SizeBytes:    pgtype.Int8{Int64: result.SizeBytes, Valid: true},
		LastModified: pgtype.Timestamptz{Time: result.LastModified, Valid: true},
	})
}

func (q *StatQueue) Fail(ctx context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error {
	return q.queries.FailIndexStat(ctx, db.FailIndexStatParams{
		FileID:        fileID,
		LastError:     pgtype.Text{String: cause, Valid: true},
		NextAttemptAt: pgtype.Timestamptz{Time: nextAttempt, Valid: true},
		Exhausted:     exhausted,
	})
}
