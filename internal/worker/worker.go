// Package worker implements a generic worker pool that drains a
// postgres-backed job queue. The pool owns all timing and retry policy
// (polling, seeding, claim TTL, backoff, max attempts); the Queue
// implementation is a thin adapter over the per-index-type SQL queries and
// the Handler performs the actual indexing work.
//
// Adding a new index type means providing a Queue (new status table +
// queries) and a Handler; the pool is reused unchanged.
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Job is one claimed unit of work: a file to index.
type Job struct {
	FileID int64
	Key    string
	// Attempts is the number of claims including this one; the pool uses it
	// to compute backoff and decide when retries are exhausted.
	Attempts int32
}

// Queue is the persistence adapter for one index type's job table. R is the
// result type the index type produces; Complete persists it together with
// the done status in one atomic write. All methods must be safe for
// concurrent use.
type Queue[R any] interface {
	// Seed enqueues files that are not yet known to this index type and
	// returns how many were added.
	Seed(ctx context.Context) (int64, error)
	// Claim atomically claims up to limit jobs. Jobs claimed before
	// staleBefore by other (presumed dead) workers may be re-claimed.
	Claim(ctx context.Context, limit int32, staleBefore time.Time) ([]Job, error)
	// Release returns a claimed-but-unstarted job to pending without
	// consuming an attempt (pool shutdown between claim and dispatch).
	Release(ctx context.Context, fileID int64) error
	// Complete marks a job done and records its result.
	Complete(ctx context.Context, fileID int64, result R) error
	// Fail records a failed attempt. When exhausted is false the job becomes
	// claimable again at nextAttempt; otherwise it is parked as an error.
	Fail(ctx context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error
}

// Handler performs the indexing work for one job and returns the result to
// persist. An error triggers the retry policy and the result is discarded.
type Handler[R any] interface {
	Handle(ctx context.Context, job Job) (R, error)
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc[R any] func(ctx context.Context, job Job) (R, error)

func (f HandlerFunc[R]) Handle(ctx context.Context, job Job) (R, error) { return f(ctx, job) }

// Config carries the pool's tuning knobs. Zero values are replaced by the
// defaults below in New.
type Config struct {
	// Workers is the number of concurrent job executors.
	Workers int
	// PollInterval is how long the dispatcher sleeps after finding the queue
	// empty. A full batch triggers an immediate re-poll to drain backlogs.
	PollInterval time.Duration
	// SeedInterval is how often the queue discovers new files.
	SeedInterval time.Duration
	// BatchSize is the maximum number of jobs claimed per poll.
	BatchSize int32
	// MaxAttempts is the number of handler attempts before a job is parked
	// as an error.
	MaxAttempts int32
	// ClaimTTL is how long a claim may be held before other workers treat it
	// as abandoned and re-claim it. Must comfortably exceed the slowest
	// expected handler run.
	ClaimTTL time.Duration
	// BackoffBase is the retry delay after the first failure; it doubles per
	// attempt up to BackoffMax.
	BackoffBase time.Duration
	// BackoffMax caps the exponential retry delay.
	BackoffMax time.Duration
}

func (c Config) withDefaults() Config {
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.SeedInterval <= 0 {
		c.SeedInterval = time.Minute
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 32
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.ClaimTTL <= 0 {
		c.ClaimTTL = 10 * time.Minute
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 10 * time.Second
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = 10 * time.Minute
	}
	return c
}

// Status writes (Complete/Fail/Release) run on a context detached from pool
// cancellation, so they are individually bounded and retried a few times:
// a transient DB blip must not lose a finished job's outcome, and a hung DB
// must not stall shutdown forever.
const (
	statusWriteTimeout = 10 * time.Second
	statusWriteTries   = 3
	statusRetryDelay   = 500 * time.Millisecond
)

// Pool polls a Queue and fans claimed jobs out to a fixed set of worker
// goroutines running the Handler.
type Pool[R any] struct {
	cfg     Config
	queue   Queue[R]
	handler Handler[R]
	name    string
	// now and retryDelay are stubbed in tests.
	now        func() time.Time
	retryDelay time.Duration
}

// New constructs a Pool. name labels log lines (e.g. "stat"). It does no
// I/O; call Run to start.
func New[R any](name string, cfg Config, queue Queue[R], handler Handler[R]) *Pool[R] {
	return &Pool[R]{
		cfg:        cfg.withDefaults(),
		queue:      queue,
		handler:    handler,
		name:       name,
		now:        time.Now,
		retryDelay: statusRetryDelay,
	}
}

// Run seeds, polls, and executes jobs until ctx is cancelled, then waits for
// in-flight jobs to finish and returns nil. Queue errors are logged and
// retried on the next tick rather than aborting the pool.
func (p *Pool[R]) Run(ctx context.Context) error {
	slog.Info("worker pool starting", "pool", p.name,
		"workers", p.cfg.Workers, "pollInterval", p.cfg.PollInterval, "batchSize", p.cfg.BatchSize)

	jobs := make(chan Job)
	var wg sync.WaitGroup
	for range p.cfg.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.work(ctx, jobs)
		}()
	}

	p.dispatch(ctx, jobs)
	close(jobs)
	wg.Wait()

	slog.Info("worker pool stopped", "pool", p.name)
	return nil
}

// dispatch is the single dispatcher loop: it seeds on SeedInterval and
// otherwise claims batches and hands them to workers, blocking (and thereby
// applying backpressure) when all workers are busy.
func (p *Pool[R]) dispatch(ctx context.Context, jobs chan<- Job) {
	p.seed(ctx)
	seedTick := time.NewTicker(p.cfg.SeedInterval)
	defer seedTick.Stop()

	for {
		claimed, err := p.queue.Claim(ctx, p.cfg.BatchSize, p.now().Add(-p.cfg.ClaimTTL))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("claim failed", "pool", p.name, "error", err)
		}

		for i, job := range claimed {
			select {
			case jobs <- job:
			case <-ctx.Done():
				// Shutdown between claim and dispatch: release the rest so
				// they are immediately claimable after restart instead of
				// waiting out the claim TTL.
				p.release(context.WithoutCancel(ctx), claimed[i:])
				return
			}
		}

		// A full batch suggests a backlog: claim again immediately.
		if int32(len(claimed)) >= p.cfg.BatchSize {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-seedTick.C:
			p.seed(ctx)
		case <-time.After(p.cfg.PollInterval):
		}
	}
}

// release returns undispatched claims to pending during shutdown.
func (p *Pool[R]) release(ctx context.Context, jobs []Job) {
	for _, job := range jobs {
		err := p.statusWrite(ctx, "release", job, func(c context.Context) error {
			return p.queue.Release(c, job.FileID)
		})
		if err != nil {
			// The claim TTL will recover the row; nothing else to do.
			slog.Error("release failed", "pool", p.name, "key", job.Key, "error", err)
		}
	}
	if len(jobs) > 0 {
		slog.Info("released undispatched jobs", "pool", p.name, "count", len(jobs))
	}
}

// statusWrite runs one queue status mutation with a per-try timeout and a
// small retry budget.
func (p *Pool[R]) statusWrite(ctx context.Context, op string, job Job, fn func(ctx context.Context) error) error {
	var err error
	for try := 1; try <= statusWriteTries; try++ {
		tryCtx, cancel := context.WithTimeout(ctx, statusWriteTimeout)
		err = fn(tryCtx)
		cancel()
		if err == nil {
			return nil
		}
		if try < statusWriteTries {
			slog.Warn("status write failed, retrying", "pool", p.name,
				"op", op, "key", job.Key, "try", try, "error", err)
			time.Sleep(p.retryDelay)
		}
	}
	return err
}

func (p *Pool[R]) seed(ctx context.Context) {
	n, err := p.queue.Seed(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("seed failed", "pool", p.name, "error", err)
		}
		return
	}
	if n > 0 {
		slog.Info("seeded jobs", "pool", p.name, "count", n)
	}
}

// work executes jobs until the jobs channel closes. Status writes use a
// context detached from cancellation so a job finishing during shutdown
// still records its outcome instead of leaving a claim to expire.
func (p *Pool[R]) work(ctx context.Context, jobs <-chan Job) {
	for job := range jobs {
		statusCtx := context.WithoutCancel(ctx)
		result, err := p.handler.Handle(ctx, job)
		if err != nil {
			p.fail(statusCtx, job, err)
			continue
		}
		err = p.statusWrite(statusCtx, "complete", job, func(c context.Context) error {
			return p.queue.Complete(c, job.FileID, result)
		})
		if err != nil {
			// The row stays claimed and will be reclaimed after the TTL;
			// the handler is idempotent so the re-run is harmless.
			slog.Error("complete failed", "pool", p.name, "key", job.Key, "error", err)
			continue
		}
		slog.Info("job done", "pool", p.name, "key", job.Key)
	}
}

func (p *Pool[R]) fail(ctx context.Context, job Job, cause error) {
	exhausted := job.Attempts >= p.cfg.MaxAttempts
	nextAttempt := p.now().Add(p.backoff(job.Attempts))
	err := p.statusWrite(ctx, "fail", job, func(c context.Context) error {
		return p.queue.Fail(c, job.FileID, cause.Error(), nextAttempt, exhausted)
	})
	if err != nil {
		slog.Error("fail-mark failed", "pool", p.name, "key", job.Key, "error", err)
		return
	}
	slog.Warn("job failed", "pool", p.name, "key", job.Key,
		"attempts", job.Attempts, "exhausted", exhausted, "error", cause)
}

// backoff returns the delay before the next retry: BackoffBase doubled per
// completed attempt, capped at BackoffMax.
func (p *Pool[R]) backoff(attempts int32) time.Duration {
	d := p.cfg.BackoffBase
	for i := int32(1); i < attempts; i++ {
		d *= 2
		if d >= p.cfg.BackoffMax {
			return p.cfg.BackoffMax
		}
	}
	return min(d, p.cfg.BackoffMax)
}
