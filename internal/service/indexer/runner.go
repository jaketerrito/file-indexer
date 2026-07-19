// Package indexer implements index types: units of work that turn a file
// into some piece of metadata (stat, and later exif, tags, ...). Each index
// type is just a ProcessFunc plus a result table; Run drives any of them
// with the same claim -> process -> record loop.
//
// There is no in-process worker pool. Scaling out means running more pods,
// not more goroutines: Run is a single-threaded loop, and any number of
// instances can run it concurrently against the same queue because claims
// use FOR UPDATE SKIP LOCKED (see index_queue.sql). This keeps the indexer
// a plain stateless workload that a HorizontalPodAutoscaler can replicate
// like any other.
package indexer

import (
	"context"
	"log/slog"
	"time"
)

// Job is one claimed unit of work: a file to index.
type Job struct {
	FileID int64
	Key    string
	// Attempts is the number of claims including this one; Run uses it to
	// compute backoff and decide when retries are exhausted.
	Attempts int32
}

// Queue is the persistence adapter for one index type. R is the result
// type the index type produces; Complete persists it together with the
// done status, atomically. PGQueue is the one production implementation,
// generic over any result type — index types do not write their own queue
// code.
type Queue[R any] interface {
	// Seed enqueues files not yet known to this index type and re-enqueues
	// done rows whose result is stale (files.marked_at has moved past the
	// stored mark). Returns how many rows changed.
	Seed(ctx context.Context) (int64, error)
	// Claim atomically claims up to limit jobs. Jobs claimed before
	// staleBefore by another (presumed dead) instance may be re-claimed.
	Claim(ctx context.Context, limit int32, staleBefore time.Time) ([]Job, error)
	// Complete marks a job done and records its result in one transaction.
	Complete(ctx context.Context, fileID int64, result R) error
	// Fail records a failed attempt. When exhausted is false the job becomes
	// claimable again at nextAttempt; otherwise it is parked as an error.
	Fail(ctx context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error
}

// ProcessFunc does the indexing work for one job and returns the result to
// persist. An error triggers the retry policy and the result is discarded.
type ProcessFunc[R any] func(ctx context.Context, job Job) (R, error)

// Config carries Run's tuning knobs. Populate from config.Load or set
// all fields explicitly.
type Config struct {
	// PollInterval is how long the loop sleeps after finding the queue
	// empty (or near-empty). A full batch triggers an immediate re-claim to
	// drain backlogs.
	PollInterval time.Duration
	// BatchSize is the maximum number of jobs claimed per poll.
	BatchSize int32
	// MaxAttempts is the number of process attempts before a job is parked
	// as an error.
	MaxAttempts int32
	// ClaimTTL is how long a claim may be held before other instances treat
	// it as abandoned and re-claim it. Must comfortably exceed the slowest
	// expected process run.
	ClaimTTL time.Duration
	// BackoffBase is the retry delay after the first failure; it doubles
	// per attempt up to BackoffMax.
	BackoffBase time.Duration
	// BackoffMax caps the exponential retry delay.
	BackoffMax time.Duration
}

// Status writes (Complete/Fail) run on a context detached from cancellation,
// so they are individually bounded and retried a few times: a transient DB
// blip must not lose a finished job's outcome, and a hung DB must not stall
// shutdown forever.
const (
	statusWriteTimeout = 10 * time.Second
	statusWriteTries   = 3
	statusRetryDelay   = 500 * time.Millisecond
)

// runner holds Run's state. now and retryDelay are stubbed in tests.
type runner[R any] struct {
	cfg     Config
	queue   Queue[R]
	process ProcessFunc[R]
	name    string

	now        func() time.Time
	retryDelay time.Duration
}

// Run claims and processes jobs sequentially until ctx is cancelled, then
// returns nil. There is no internal concurrency: throughput comes from
// running more Run instances (more pods), not more goroutines per instance.
// Queue errors are logged and retried on the next iteration rather than
// aborting the loop.
func Run[R any](ctx context.Context, name string, cfg Config, queue Queue[R], process ProcessFunc[R]) error {
	r := &runner[R]{
		cfg:        cfg,
		queue:      queue,
		process:    process,
		name:       name,
		now:        time.Now,
		retryDelay: statusRetryDelay,
	}
	return r.run(ctx)
}

func (r *runner[R]) run(ctx context.Context) error {
	slog.Info("indexer starting", "index", r.name,
		"pollInterval", r.cfg.PollInterval, "batchSize", r.cfg.BatchSize)

	r.seed(ctx)

	for {
		claimed, err := r.queue.Claim(ctx, r.cfg.BatchSize, r.now().Add(-r.cfg.ClaimTTL))
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("indexer stopped", "index", r.name)
				return nil
			}
			slog.Error("claim failed", "index", r.name, "error", err)
		}

		for _, job := range claimed {
			if ctx.Err() != nil {
				// Shutdown mid-batch: the rest stay claimed and are picked
				// up by any instance once the claim TTL expires, rather
				// than racing an explicit release against the shutdown
				// deadline.
				slog.Info("indexer stopped", "index", r.name)
				return nil
			}
			r.handle(ctx, job)
		}

		// A full batch suggests a backlog: claim again immediately.
		if int32(len(claimed)) >= r.cfg.BatchSize {
			continue
		}

		// Queue looked drained: this is a cheap point to look for new or
		// stale work before sleeping.
		r.seed(ctx)

		select {
		case <-ctx.Done():
			slog.Info("indexer stopped", "index", r.name)
			return nil
		case <-time.After(r.cfg.PollInterval):
		}
	}
}

func (r *runner[R]) seed(ctx context.Context) {
	n, err := r.queue.Seed(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("seed failed", "index", r.name, "error", err)
		}
		return
	}
	if n > 0 {
		slog.Info("seeded jobs", "index", r.name, "count", n)
	}
}

// handle processes one job and records its outcome. Status writes use a
// context detached from cancellation so a job finishing during shutdown
// still records its outcome instead of leaving a claim to expire.
func (r *runner[R]) handle(ctx context.Context, job Job) {
	statusCtx := context.WithoutCancel(ctx)
	result, err := r.process(ctx, job)
	if err != nil {
		r.fail(statusCtx, job, err)
		return
	}
	err = r.statusWrite(statusCtx, "complete", job, func(c context.Context) error {
		return r.queue.Complete(c, job.FileID, result)
	})
	if err != nil {
		// The row stays claimed and will be reclaimed after the TTL; the
		// process function is idempotent so the re-run is harmless.
		slog.Error("complete failed", "index", r.name, "key", job.Key, "error", err)
		return
	}
	slog.Info("job done", "index", r.name, "key", job.Key)
}

func (r *runner[R]) fail(ctx context.Context, job Job, cause error) {
	exhausted := job.Attempts >= r.cfg.MaxAttempts
	nextAttempt := r.now().Add(r.backoff(job.Attempts))
	err := r.statusWrite(ctx, "fail", job, func(c context.Context) error {
		return r.queue.Fail(c, job.FileID, cause.Error(), nextAttempt, exhausted)
	})
	if err != nil {
		slog.Error("fail-mark failed", "index", r.name, "key", job.Key, "error", err)
		return
	}
	slog.Warn("job failed", "index", r.name, "key", job.Key,
		"attempts", job.Attempts, "exhausted", exhausted, "error", cause)
}

// backoff returns the delay before the next retry: BackoffBase doubled per
// completed attempt, capped at BackoffMax.
func (r *runner[R]) backoff(attempts int32) time.Duration {
	d := r.cfg.BackoffBase
	for i := int32(1); i < attempts; i++ {
		d *= 2
		if d >= r.cfg.BackoffMax {
			return r.cfg.BackoffMax
		}
	}
	return min(d, r.cfg.BackoffMax)
}

// statusWrite runs one queue status mutation with a per-try timeout and a
// small retry budget.
func (r *runner[R]) statusWrite(ctx context.Context, op string, job Job, fn func(ctx context.Context) error) error {
	var err error
	for try := 1; try <= statusWriteTries; try++ {
		tryCtx, cancel := context.WithTimeout(ctx, statusWriteTimeout)
		err = fn(tryCtx)
		cancel()
		if err == nil {
			return nil
		}
		if try < statusWriteTries {
			slog.Warn("status write failed, retrying", "index", r.name,
				"op", op, "key", job.Key, "try", try, "error", err)
			time.Sleep(r.retryDelay)
		}
	}
	return err
}
