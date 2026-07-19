package indexer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fastConfig returns a config with intervals small enough that tests finish
// quickly while still exercising the real loop.
func fastConfig() Config {
	return Config{
		PollInterval: time.Millisecond,
		BatchSize:    8,
		MaxAttempts:  3,
		ClaimTTL:     time.Minute,
		BackoffBase:  time.Second,
		BackoffMax:   8 * time.Second,
	}
}

// testConfig returns a Config ready to pass to Run, with test seams set to
// sensible defaults.
func testConfig() Config {
	return Config{
		PollInterval: time.Millisecond,
		BatchSize:    8,
		MaxAttempts:  3,
		ClaimTTL:     time.Minute,
		BackoffBase:  time.Second,
		BackoffMax:   8 * time.Second,
		now:          time.Now,
		retryDelay:   500 * time.Millisecond,
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
	mu           sync.Mutex
	jobs         []Job
	claimErrs    int // number of leading Claim calls that fail
	seedErr      error
	seedN        int64
	completeErrs int // number of leading Complete calls that fail
	completeErr  error

	seeds     int
	claims    int
	completed []int64
	fails     []failCall

	activity chan struct{} // signalled on every Complete/Fail call
}

func newFakeQueue(jobs ...Job) *fakeQueue {
	return &fakeQueue{jobs: jobs, activity: make(chan struct{}, 64)}
}

func (q *fakeQueue) Seed(_ context.Context) (int64, error) {
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

func (q *fakeQueue) Complete(_ context.Context, fileID int64, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, fileID)
	q.activity <- struct{}{}
	if q.completeErrs > 0 {
		q.completeErrs--
		return errors.New("complete boom")
	}
	return q.completeErr
}

func (q *fakeQueue) Fail(_ context.Context, fileID int64, cause string, nextAttempt time.Time, exhausted bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fails = append(q.fails, failCall{fileID, cause, nextAttempt, exhausted})
	q.activity <- struct{}{}
	return nil
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

// runRunner starts Run in a goroutine and returns a stop function that
// cancels the context and waits for Run to return.
func runRunner(t *testing.T, cfg Config, q Queue[string], process ProcessFunc[string]) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, "test", cfg, q, process) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("runner did not shut down")
		}
	}
}

func TestRunProcessesJobsSequentially(t *testing.T) {
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1}, Job{FileID: 2, Key: "b", Attempts: 1})

	var mu sync.Mutex
	var handled []string
	process := ProcessFunc[string](func(_ context.Context, job Job) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		handled = append(handled, job.Key)
		return "ok", nil
	})

	stop := runRunner(t, testConfig(), q, process)
	q.waitActivity(t, 2)
	stop()

	mu.Lock()
	defer mu.Unlock()
	if len(handled) != 2 || handled[0] != "a" || handled[1] != "b" {
		t.Fatalf("handled = %v, want [a b] in claim order", handled)
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
		t.Error("runner never seeded on startup")
	}
}

func TestRunFailsJobWithBackoff(t *testing.T) {
	q := newFakeQueue(Job{FileID: 7, Key: "bad", Attempts: 2})
	process := ProcessFunc[string](func(context.Context, Job) (string, error) { return "", errors.New("stat exploded") })

	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	cfg := testConfig()
	cfg.now = func() time.Time { return base }

	stop := runRunner(t, cfg, q, process)
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

func TestRunExhaustsRetries(t *testing.T) {
	q := newFakeQueue(Job{FileID: 7, Key: "bad", Attempts: 3}) // == MaxAttempts
	process := ProcessFunc[string](func(context.Context, Job) (string, error) { return "", errors.New("still broken") })

	stop := runRunner(t, testConfig(), q, process)
	q.waitActivity(t, 1)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.fails) != 1 || !q.fails[0].exhausted {
		t.Fatalf("fails = %+v, want one exhausted failure", q.fails)
	}
}

func TestRunSurvivesClaimAndSeedErrors(t *testing.T) {
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1})
	q.claimErrs = 2
	q.seedErr = errors.New("seed boom")

	process := ProcessFunc[string](func(context.Context, Job) (string, error) { return "ok", nil })

	stop := runRunner(t, testConfig(), q, process)
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

func TestRunFullBatchClaimsAgainImmediately(t *testing.T) {
	// With BatchSize 1 and a poll interval far longer than the test, both
	// jobs only complete if a full batch triggers an immediate re-claim.
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1}, Job{FileID: 2, Key: "b", Attempts: 1})
	process := ProcessFunc[string](func(context.Context, Job) (string, error) { return "ok", nil })

	cfg := testConfig()
	cfg.BatchSize = 1
	cfg.PollInterval = time.Hour

	stop := runRunner(t, cfg, q, process)
	q.waitActivity(t, 2)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 2 {
		t.Fatalf("completed = %v, want both jobs", q.completed)
	}
}

func TestRunSeedsAfterShortBatch(t *testing.T) {
	// A short (non-full) batch must trigger a seed before the next sleep,
	// with no interval/ticker involved.
	q := newFakeQueue()
	process := ProcessFunc[string](func(context.Context, Job) (string, error) { return "ok", nil })

	stop := runRunner(t, testConfig(), q, process)
	deadline := time.Now().Add(5 * time.Second)
	for {
		q.mu.Lock()
		seeds := q.seeds
		q.mu.Unlock()
		if seeds >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runner never re-seeded after a short batch")
		}
		time.Sleep(time.Millisecond)
	}
	stop()
}

func TestRunRetriesTransientStatusWriteError(t *testing.T) {
	// A transient Complete failure is retried and must not lose the
	// outcome or trigger the failure path.
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1})
	q.completeErrs = 1

	process := ProcessFunc[string](func(context.Context, Job) (string, error) { return "ok", nil })

	cfg := testConfig()
	cfg.retryDelay = time.Millisecond

	stop := runRunner(t, cfg, q, process)
	q.waitActivity(t, 2)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 2 {
		t.Fatalf("Complete calls = %v, want 2 (failure then retry)", q.completed)
	}
	if len(q.fails) != 0 {
		t.Errorf("fails = %v, want none", q.fails)
	}
}

func TestRunLogsStatusWriteErrors(t *testing.T) {
	// Persistent Complete errors exhaust the retry budget without crashing
	// the loop or blocking subsequent jobs.
	q := newFakeQueue(Job{FileID: 1, Key: "a", Attempts: 1}, Job{FileID: 2, Key: "b", Attempts: 1})
	q.completeErr = errors.New("db down")

	process := ProcessFunc[string](func(context.Context, Job) (string, error) { return "ok", nil })

	cfg := testConfig()
	cfg.retryDelay = time.Millisecond

	stop := runRunner(t, cfg, q, process)
	q.waitActivity(t, 2*statusWriteTries)
	stop()

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 2*statusWriteTries {
		t.Fatalf("Complete calls = %d, want %d (retry budget per job)", len(q.completed), 2*statusWriteTries)
	}
}

func TestRunStopsMidBatchOnCancel(t *testing.T) {
	// One blocking job: the loop claims all three, processes the first,
	// and must stop before the second once ctx is cancelled instead of
	// racing to finish the whole batch.
	q := newFakeQueue(
		Job{FileID: 1, Key: "a", Attempts: 1},
		Job{FileID: 2, Key: "b", Attempts: 1},
		Job{FileID: 3, Key: "c", Attempts: 1},
	)

	started := make(chan struct{})
	unblock := make(chan struct{})
	process := ProcessFunc[string](func(context.Context, Job) (string, error) {
		close(started) // only job 1 is ever processed
		<-unblock
		return "ok", nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, "test", testConfig(), q, process) }()

	<-started
	cancel()
	close(unblock)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not shut down")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 1 || q.completed[0] != 1 {
		t.Errorf("completed = %v, want only job 1 (in-flight job finishes and records)", q.completed)
	}
}

func TestBackoff(t *testing.T) {
	cfg := Config{BackoffBase: time.Second, BackoffMax: 10 * time.Second}
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
		if got := backoff(cfg, tt.attempts); got != tt.want {
			t.Errorf("backoff(%d) = %v, want %v", tt.attempts, got, tt.want)
		}
	}
}
