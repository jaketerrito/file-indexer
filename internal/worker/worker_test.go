package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fastConfig returns a config with intervals small enough that tests finish
// quickly while still exercising the real loops.
func fastConfig() Config {
	return Config{
		Workers:      2,
		PollInterval: time.Millisecond,
		SeedInterval: time.Hour, // tests trigger seeding explicitly via start-up seed
		BatchSize:    8,
		MaxAttempts:  3,
		ClaimTTL:     time.Minute,
		BackoffBase:  time.Second,
		BackoffMax:   8 * time.Second,
	}
}

type failCall struct {
	fileID      int64
	cause       string
	nextAttempt time.Time
	exhausted   bool
}

// fakeQueue is an in-memory Queue. Claim hands out the queued jobs once;
// afterwards it returns empty batches (or claimErrs, if set).
type fakeQueue struct {
	mu        sync.Mutex
	jobs      []Job
	claimErrs int // number of leading Claim calls that fail
	seedErr   error
	seedN     int64

	seeds       int
	claims      int
	completed   []int64
	fails       []failCall
	completeErr error
	failErr     error

	activity chan struct{} // signalled on every Complete/Fail
}

func newFakeQueue(jobs ...Job) *fakeQueue {
	return &fakeQueue{jobs: jobs, activity: make(chan struct{}, 64)}
}

func (q *fakeQueue) Seed(context.Context) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seeds++
	return q.seedN, q.seedErr
}

func (q *fakeQueue) Claim(_ context.Context, limit int32, _ time.Time) ([]Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claims++
	if q.claimErrs > 0 {
		q.claimErrs--
		return nil, errors.New("claim boom")
	}
	n := min(int(limit), len(q.jobs))
	batch := q.jobs[:n]
	q.jobs = q.jobs[n:]
	return batch, nil
}

func (q *fakeQueue) Complete(_ context.Context, fileID int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, fileID)
	q.activity <- struct{}{}
	return q.completeErr
}

func (q *fakeQueue) Fail(_ context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fails = append(q.fails, failCall{fileID, cause, nextAttempt, exhausted})
	q.activity <- struct{}{}
	return q.failErr
}

// waitActivity blocks until the queue has seen n Complete/Fail calls.
func (q *fakeQueue) waitActivity(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-q.activity:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for job outcomes")
		}
	}
}

// runPool starts the pool and returns a stop function that cancels it and
// waits for Run to return.
func runPool(t *testing.T, p *Pool) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("pool did not shut down")
		}
	}
}

func TestPoolProcessesJobs(t *testing.T) {
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1}, Job{FileID: 2, Key: "b", Attempts: 1})

	var mu sync.Mutex
	var handled []string
	h := HandlerFunc(func(_ context.Context, job Job) error {
		mu.Lock()
		defer mu.Unlock()
		handled = append(handled, job.Key)
		return nil
	})

	stop := runPool(t, New("test", fastConfig(), q, h))
	q.waitActivity(t, 2)
	stop()

	mu.Lock()
	defer mu.Unlock()
	if len(handled) != 2 {
		t.Fatalf("handled %v, want 2 jobs", handled)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 2 {
		t.Errorf("completed = %v, want ids 1 and 2", q.completed)
	}
	if len(q.fails) != 0 {
		t.Errorf("fails = %v, want none", q.fails)
	}
	if q.seeds == 0 {
		t.Error("pool never seeded on startup")
	}
}

func TestPoolFailsJobWithBackoff(t *testing.T) {
	q := newFakeQueue(Job{FileID: 7, Key: "bad", Attempts: 2})
	h := HandlerFunc(func(context.Context, Job) error { return errors.New("stat exploded") })

	cfg := fastConfig()
	p := New("test", cfg, q, h)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return base }

	stop := runPool(t, p)
	q.waitActivity(t, 1)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.fails) != 1 {
		t.Fatalf("fails = %v, want exactly 1", q.fails)
	}
	f := q.fails[0]
	if f.fileID != 7 || f.cause != "stat exploded" {
		t.Errorf("fail = %+v, want fileID 7, cause 'stat exploded'", f)
	}
	if f.exhausted {
		t.Error("exhausted = true, want false (attempts 2 < max 3)")
	}
	// Attempt 2 backs off BackoffBase*2.
	if want := base.Add(2 * time.Second); !f.nextAttempt.Equal(want) {
		t.Errorf("nextAttempt = %v, want %v", f.nextAttempt, want)
	}
	if len(q.completed) != 0 {
		t.Errorf("completed = %v, want none", q.completed)
	}
}

func TestPoolExhaustsRetries(t *testing.T) {
	q := newFakeQueue(Job{FileID: 7, Key: "bad", Attempts: 3}) // == MaxAttempts
	h := HandlerFunc(func(context.Context, Job) error { return errors.New("still broken") })

	stop := runPool(t, New("test", fastConfig(), q, h))
	q.waitActivity(t, 1)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.fails) != 1 || !q.fails[0].exhausted {
		t.Fatalf("fails = %+v, want one exhausted failure", q.fails)
	}
}

func TestPoolSurvivesClaimAndSeedErrors(t *testing.T) {
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1})
	q.claimErrs = 2
	q.seedErr = errors.New("seed boom")

	h := HandlerFunc(func(context.Context, Job) error { return nil })

	stop := runPool(t, New("test", fastConfig(), q, h))
	q.waitActivity(t, 1)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 1 {
		t.Fatalf("completed = %v, want the job processed after claim errors", q.completed)
	}
	if q.claims < 3 {
		t.Errorf("claims = %d, want >= 3 (two failures then success)", q.claims)
	}
}

func TestPoolFullBatchClaimsAgainImmediately(t *testing.T) {
	// With BatchSize 1 and a poll interval far longer than the test, both
	// jobs only complete if a full batch triggers an immediate re-claim.
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1}, Job{FileID: 2, Key: "b", Attempts: 1})
	h := HandlerFunc(func(context.Context, Job) error { return nil })

	cfg := fastConfig()
	cfg.BatchSize = 1
	cfg.PollInterval = time.Hour

	stop := runPool(t, New("test", cfg, q, h))
	q.waitActivity(t, 2)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 2 {
		t.Fatalf("completed = %v, want both jobs", q.completed)
	}
}

func TestPoolSeedsOnInterval(t *testing.T) {
	q := newFakeQueue()
	h := HandlerFunc(func(context.Context, Job) error { return nil })

	cfg := fastConfig()
	cfg.SeedInterval = time.Millisecond

	stop := runPool(t, New("test", cfg, q, h))
	deadline := time.Now().Add(5 * time.Second)
	for {
		q.mu.Lock()
		seeds := q.seeds
		q.mu.Unlock()
		if seeds >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pool never re-seeded on interval")
		}
		time.Sleep(time.Millisecond)
	}
	stop()
}

func TestPoolLogsStatusWriteErrors(t *testing.T) {
	// Complete/Fail persistence errors must not crash the pool or block
	// subsequent jobs.
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1}, Job{FileID: 2, Key: "b", Attempts: 1})
	q.completeErr = errors.New("db down")

	h := HandlerFunc(func(context.Context, Job) error { return nil })

	stop := runPool(t, New("test", fastConfig(), q, h))
	q.waitActivity(t, 2)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 2 {
		t.Fatalf("completed attempts = %v, want both despite errors", q.completed)
	}
}

func TestBackoff(t *testing.T) {
	p := New("test", Config{BackoffBase: time.Second, BackoffMax: 10 * time.Second}, nil, nil)
	tests := []struct {
		attempts int32
		want     time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second}, // capped
		{50, 10 * time.Second},
	}
	for _, tt := range tests {
		if got := p.backoff(tt.attempts); got != tt.want {
			t.Errorf("backoff(%d) = %v, want %v", tt.attempts, got, tt.want)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	if got.Workers <= 0 || got.PollInterval <= 0 || got.SeedInterval <= 0 ||
		got.BatchSize <= 0 || got.MaxAttempts <= 0 || got.ClaimTTL <= 0 ||
		got.BackoffBase <= 0 || got.BackoffMax <= 0 {
		t.Errorf("withDefaults left zero fields: %+v", got)
	}

	// Explicit values survive.
	cfg := Config{Workers: 9, BatchSize: 3}
	if got := cfg.withDefaults(); got.Workers != 9 || got.BatchSize != 3 {
		t.Errorf("withDefaults overwrote explicit values: %+v", got)
	}
}
