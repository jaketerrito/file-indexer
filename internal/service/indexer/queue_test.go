package indexer

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

// pgQueueForTest builds a PGQueue around a mock Queries, bypassing
// NewPGQueue so no real *pgxpool.Pool is needed. Complete (which needs a
// real transaction) is covered by the db package's integration tests
// instead.
func pgQueueForTest(queries Queries) *PGQueue[StatResult] {
	return &PGQueue[StatResult]{queries: queries, indexType: "stat", store: StoreStatResult}
}

func TestPGQueueSeed(t *testing.T) {
	queries := NewMockQueries(t)
	queries.EXPECT().SeedIndexQueue(mock.Anything, "stat").Return(3, nil)
	queries.EXPECT().RequeueStaleIndexQueue(mock.Anything, "stat").Return(2, nil)

	n, err := pgQueueForTest(queries).Seed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Seed returns seeded + requeued combined.
	if n != 5 {
		t.Errorf("Seed = %d, want 5 (3 seeded + 2 requeued)", n)
	}
}

func TestPGQueueSeedPropagatesSeedError(t *testing.T) {
	queries := NewMockQueries(t)
	queries.EXPECT().SeedIndexQueue(mock.Anything, "stat").Return(0, errors.New("db down"))

	if _, err := pgQueueForTest(queries).Seed(context.Background()); err == nil {
		t.Fatal("expected error from SeedIndexQueue")
	}
}

func TestPGQueueSeedPropagatesRequeueError(t *testing.T) {
	queries := NewMockQueries(t)
	queries.EXPECT().SeedIndexQueue(mock.Anything, "stat").Return(0, nil)
	queries.EXPECT().RequeueStaleIndexQueue(mock.Anything, "stat").Return(0, errors.New("db down"))

	if _, err := pgQueueForTest(queries).Seed(context.Background()); err == nil {
		t.Fatal("expected error from RequeueStaleIndexQueue")
	}
}

func TestPGQueueClaim(t *testing.T) {
	queries := NewMockQueries(t)
	queries.EXPECT().
		ClaimIndexQueue(mock.Anything, mock.MatchedBy(func(arg db.ClaimIndexQueueParams) bool {
			return arg.IndexType == "stat" && arg.BatchSize == 5 &&
				arg.StaleTimeout.Microseconds == int64(4*time.Hour.Microseconds())
		})).
		Return([]db.ClaimIndexQueueRow{
			{FileID: 1, Attempts: 1, Key: "a"},
			{FileID: 2, Attempts: 4, Key: "b"},
		}, nil)

	jobs, err := pgQueueForTest(queries).Claim(context.Background(), 5, 4*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	want := []Job{
		{FileID: 1, Key: "a", Attempts: 1},
		{FileID: 2, Key: "b", Attempts: 4},
	}
	if len(jobs) != len(want) {
		t.Fatalf("Claim returned %d jobs, want %d", len(jobs), len(want))
	}
	for i := range want {
		if jobs[i] != want[i] {
			t.Errorf("jobs[%d] = %+v, want %+v", i, jobs[i], want[i])
		}
	}
}

func TestPGQueueClaimError(t *testing.T) {
	queries := NewMockQueries(t)
	queries.EXPECT().ClaimIndexQueue(mock.Anything, mock.Anything).Return(nil, errors.New("db down"))

	if _, err := pgQueueForTest(queries).Claim(context.Background(), 1, time.Hour); err == nil {
		t.Fatal("expected error")
	}
}

func TestPGQueueFail(t *testing.T) {
	next := time.Date(2026, 7, 18, 12, 5, 0, 0, time.UTC)

	queries := NewMockQueries(t)
	queries.EXPECT().
		FailIndexQueue(mock.Anything, db.FailIndexQueueParams{
			IndexType:     "stat",
			FileID:        9,
			LastError:     pgtype.Text{String: "boom", Valid: true},
			NextAttemptAt: pgtype.Timestamptz{Time: next, Valid: true},
			Exhausted:     true,
		}).
		Return(nil)

	if err := pgQueueForTest(queries).Fail(context.Background(), 9, "boom", next, true); err != nil {
		t.Fatal(err)
	}
}
