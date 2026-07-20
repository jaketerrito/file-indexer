// Package indexer implements index types: units of work that turn a file
// into some piece of metadata (stat, exif, tags, …). Each index type is
// a ProcessFunc plus a result table; Run drives any of them.
//
// Run is a single-threaded loop; scale out by running more pods.
// Claims use FOR UPDATE SKIP LOCKED so any number of instances can
// run concurrently against the same queue.
package indexer

import (
	"context"
	"log/slog"
	"time"
)

// Job is one claimed unit of work: a file to index.
type Job struct {
	FileID   int64
	Key      string
	Attempts int32 // used for backoff and exhaustion check
}

// Queue is the persistence adapter for one index type. R is the result
// type the index type produces.
type Queue[R any] interface {
	Seed(ctx context.Context) (int64, error)
	Claim(ctx context.Context, limit int32, staleBefore time.Time) ([]Job, error)
	Complete(ctx context.Context, fileID int64, result R) error
	Fail(ctx context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error
}

// ProcessFunc does the indexing work for one job.
type ProcessFunc[R any] func(ctx context.Context, job Job) (R, error)

// Config carries Run's tuning knobs.
type Config struct {
	PollInterval time.Duration
	BatchSize    int32
	MaxAttempts  int32
	ClaimTTL     time.Duration
	BackoffBase  time.Duration
	BackoffMax   time.Duration
}

// Test hooks — set only by tests inside the indexer package.
var testNow func() time.Time
var testRetryDelay time.Duration

const (
	statusWriteTimeout = 10 * time.Second
	statusWriteTries   = 3
)

// Run claims and processes jobs sequentially until ctx is cancelled, then
// returns nil. There is no internal concurrency; throughput comes from
// running more instances (more pods), not more goroutines.
// Queue errors are logged and retried on the next iteration rather than
// aborting the loop.
func Run[R any](ctx context.Context, name string, cfg Config, queue Queue[R], process ProcessFunc[R]) error {
	now := testNow
	if now == nil {
		now = time.Now
	}
	retryDelay := testRetryDelay
	if retryDelay == 0 {
		retryDelay = 500 * time.Millisecond
	}

	// statusWrite runs one queue status mutation with a per-try timeout
	// and a small retry budget, so a transient DB blip does not lose
	// a finished job's outcome.
	statusWrite := func(ctx context.Context, op string, job Job, fn func(context.Context) error) error {
		var err error
		for try := 1; try <= statusWriteTries; try++ {
			tryCtx, cancel := context.WithTimeout(ctx, statusWriteTimeout)
			err = fn(tryCtx)
			cancel()
			if err == nil {
				return nil
			}
			if try < statusWriteTries {
				slog.Warn("status write failed, retrying", "index", name,
					"op", op, "key", job.Key, "try", try, "error", err)
				time.Sleep(retryDelay)
			}
		}
		return err
	}

	// fail records a failed attempt and schedules a retry with backoff.
	fail := func(ctx context.Context, job Job, cause error) {
		exhausted := job.Attempts >= cfg.MaxAttempts
		nextAttempt := now().Add(backoff(cfg, job.Attempts))
		err := statusWrite(ctx, "fail", job, func(c context.Context) error {
			return queue.Fail(c, job.FileID, cause.Error(), nextAttempt, exhausted)
		})
		if err != nil {
			slog.Error("fail-mark failed", "index", name, "key", job.Key, "error", err)
			return
		}
		slog.Warn("job failed", "index", name, "key", job.Key,
			"attempts", job.Attempts, "exhausted", exhausted, "error", cause)
	}

	slog.Info("indexer starting", "index", name,
		"pollInterval", cfg.PollInterval, "batchSize", cfg.BatchSize)

	// Initial seed: enqueue files not yet indexed and re-enqueue stale results.
	if n, err := queue.Seed(ctx); err != nil {
		if ctx.Err() == nil {
			slog.Error("seed failed", "index", name, "error", err)
		}
	} else if n > 0 {
		slog.Info("seeded jobs", "index", name, "count", n)
	}

	for {
		claimed, err := queue.Claim(ctx, cfg.BatchSize, now().Add(-cfg.ClaimTTL))
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("indexer stopped", "index", name)
				return nil
			}
			slog.Error("claim failed", "index", name, "error", err)
		}

		for _, job := range claimed {
			if ctx.Err() != nil {
				// Shutdown mid-batch: the rest stay claimed and are picked
				// up by any instance once the claim TTL expires.
				slog.Info("indexer stopped", "index", name)
				return nil
			}

			// Status writes use a context detached from cancellation so a
			// job finishing during shutdown still records its outcome.
			statusCtx := context.WithoutCancel(ctx)
			result, pErr := process(ctx, job)
			if pErr != nil {
				fail(statusCtx, job, pErr)
				continue
			}

			err = statusWrite(statusCtx, "complete", job, func(c context.Context) error {
				return queue.Complete(c, job.FileID, result)
			})
			if err != nil {
				// The row stays claimed; process is idempotent so the
				// re-run after TTL expiry is harmless.
				slog.Error("complete failed", "index", name, "key", job.Key, "error", err)
				continue
			}
			slog.Info("job done", "index", name, "key", job.Key)
		}

		// A full batch suggests a backlog: claim again immediately.
		if int32(len(claimed)) >= cfg.BatchSize {
			continue
		}

		// Queue looked drained: look for new or stale work before sleeping.
		if n, err := queue.Seed(ctx); err != nil {
			if ctx.Err() == nil {
				slog.Error("seed failed", "index", name, "error", err)
			}
		} else if n > 0 {
			slog.Info("seeded jobs", "index", name, "count", n)
		}

		select {
		case <-ctx.Done():
			slog.Info("indexer stopped", "index", name)
			return nil
		case <-time.After(cfg.PollInterval):
		}
	}
}

// backoff returns the delay before the next retry: BackoffBase doubled per
// completed attempt, capped at BackoffMax.
func backoff(cfg Config, attempts int32) time.Duration {
	d := cfg.BackoffBase
	for i := int32(1); i < attempts; i++ {
		d *= 2
		if d >= cfg.BackoffMax {
			return cfg.BackoffMax
		}
	}
	return min(d, cfg.BackoffMax)
}
