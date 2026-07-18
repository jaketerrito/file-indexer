package indexer

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/worker"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

func TestStatQueueSeed(t *testing.T) {
	queries := NewMockStatQueries(t)
	queries.EXPECT().SeedIndexStat(mock.Anything).Return(3, nil)

	n, err := NewStatQueue(queries).Seed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("Seed = %d, want 3", n)
	}
}

func TestStatQueueClaim(t *testing.T) {
	staleBefore := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	queries := NewMockStatQueries(t)
	queries.EXPECT().
		ClaimIndexStat(mock.Anything, mock.MatchedBy(func(arg db.ClaimIndexStatParams) bool {
			return arg.BatchSize == 5 &&
				arg.StaleBefore.Valid && arg.StaleBefore.Time.Equal(staleBefore)
		})).
		Return([]db.ClaimIndexStatRow{
			{FileID: 1, Attempts: 1, Key: "a"},
			{FileID: 2, Attempts: 4, Key: "b"},
		}, nil)

	jobs, err := NewStatQueue(queries).Claim(context.Background(), 5, staleBefore)
	if err != nil {
		t.Fatal(err)
	}
	want := []worker.Job{
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

func TestStatQueueClaimError(t *testing.T) {
	queries := NewMockStatQueries(t)
	queries.EXPECT().ClaimIndexStat(mock.Anything, mock.Anything).Return(nil, errors.New("db down"))

	if _, err := NewStatQueue(queries).Claim(context.Background(), 1, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestStatQueueComplete(t *testing.T) {
	queries := NewMockStatQueries(t)
	queries.EXPECT().CompleteIndexStat(mock.Anything, int64(9)).Return(nil)

	if err := NewStatQueue(queries).Complete(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
}

func TestStatQueueFail(t *testing.T) {
	next := time.Date(2026, 7, 18, 12, 5, 0, 0, time.UTC)

	queries := NewMockStatQueries(t)
	queries.EXPECT().
		FailIndexStat(mock.Anything, db.FailIndexStatParams{
			FileID:        9,
			LastError:     pgtype.Text{String: "boom", Valid: true},
			NextAttemptAt: pgtype.Timestamptz{Time: next, Valid: true},
			Exhausted:     true,
		}).
		Return(nil)

	if err := NewStatQueue(queries).Fail(context.Background(), 9, "boom", next, true); err != nil {
		t.Fatal(err)
	}
}
