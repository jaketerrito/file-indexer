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
	ClaimIndexStat(ctx context.Context, arg db.ClaimIndexStatParams) ([]db.ClaimIndexStatRow, error)
	CompleteIndexStat(ctx context.Context, fileID int64) error
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

var _ worker.Queue = (*StatQueue)(nil)

func (q *StatQueue) Seed(ctx context.Context) (int64, error) {
	return q.queries.SeedIndexStat(ctx)
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

func (q *StatQueue) Complete(ctx context.Context, fileID int64) error {
	return q.queries.CompleteIndexStat(ctx, fileID)
}

func (q *StatQueue) Fail(ctx context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error {
	return q.queries.FailIndexStat(ctx, db.FailIndexStatParams{
		FileID:        fileID,
		LastError:     pgtype.Text{String: cause, Valid: true},
		NextAttemptAt: pgtype.Timestamptz{Time: nextAttempt, Valid: true},
		Exhausted:     exhausted,
	})
}
