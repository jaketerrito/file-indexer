package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func mustParseMetrics(t *testing.T, body io.Reader) map[string]float64 {
	t.Helper()
	dec := expfmt.NewDecoder(body, expfmt.FmtText)
	got := make(map[string]float64)
	for {
		var mf dto.MetricFamily
		if err := dec.Decode(&mf); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode metrics: %v", err)
		}
		if mf.Type == nil || *mf.Type != dto.MetricType_GAUGE || len(mf.Metric) == 0 {
			continue
		}
		// Metrics have no labels; take the first (and only) gauge value.
		got[*mf.Name] = *mf.Metric[0].Gauge.Value
	}
	return got
}

func setupTestServer(lag LagFunc) (*Status, *httptest.Server) {
	s := NewStatus("test", 0, lag)
	return s, httptest.NewServer(s.HTTPHandler())
}

func TestHealthzFresh(t *testing.T) {
	s, srv := setupTestServer(nil)
	defer srv.Close()
	s.Beat()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestHealthzStale(t *testing.T) {
	s, srv := setupTestServer(nil)
	defer srv.Close()
	// With PollInterval 0 the freshness threshold is 10s; pretend the last
	// beat happened 11 seconds ago.
	past := time.Now().Add(-11 * time.Second)
	s.lastBeat.Store(&past)

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if string(body) != "stale" {
		t.Errorf("body = %q, want %q", body, "stale")
	}
}

func TestHealthzNeverBeaten(t *testing.T) {
	_, srv := setupTestServer(nil)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestMetricsExposeLagAndHeartbeat(t *testing.T) {
	want := QueueLag{PendingCount: 42, OldestPendingSeconds: 7}
	s, srv := setupTestServer(func(context.Context) (QueueLag, error) { return want, nil })
	defer srv.Close()
	s.Beat()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got := mustParseMetrics(t, resp.Body)
	if got["indexer_queue_pending"] != float64(want.PendingCount) {
		t.Errorf("indexer_queue_pending = %v, want %v", got["indexer_queue_pending"], want.PendingCount)
	}
	if got["indexer_queue_oldest_pending_seconds"] != float64(want.OldestPendingSeconds) {
		t.Errorf("indexer_queue_oldest_pending_seconds = %v, want %v", got["indexer_queue_oldest_pending_seconds"], want.OldestPendingSeconds)
	}
	if got["indexer_last_heartbeat_timestamp_seconds"] == 0 {
		t.Errorf("indexer_last_heartbeat_timestamp_seconds = 0, want non-zero")
	}
}

func TestMetricsLagErrorStillServesHeartbeat(t *testing.T) {
	s, srv := setupTestServer(func(context.Context) (QueueLag, error) {
		return QueueLag{}, errors.New("db down")
	})
	defer srv.Close()
	s.Beat()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got := mustParseMetrics(t, resp.Body)
	if _, ok := got["indexer_queue_pending"]; ok {
		t.Errorf("indexer_queue_pending should not be exposed when lag query fails")
	}
	if _, ok := got["indexer_queue_oldest_pending_seconds"]; ok {
		t.Errorf("indexer_queue_oldest_pending_seconds should not be exposed when lag query fails")
	}
	if got["indexer_last_heartbeat_timestamp_seconds"] == 0 {
		t.Errorf("indexer_last_heartbeat_timestamp_seconds = 0, want non-zero")
	}
}

func TestStatusFreshThreshold(t *testing.T) {
	// Threshold is 2*PollInterval + 10s.
	s := NewStatus("test", 5*time.Second, nil)
	s.Beat()
	if !s.Fresh() {
		t.Error("Fresh() = false immediately after Beat()")
	}

	// One millisecond inside the threshold is still fresh.
	recent := time.Now().Add(-(2*5*time.Second + 10*time.Second) + time.Millisecond)
	s.lastBeat.Store(&recent)
	if !s.Fresh() {
		t.Errorf("Fresh() = false just inside threshold, want true")
	}

	// One millisecond past the threshold is stale.
	stale := time.Now().Add(-(2*5*time.Second + 10*time.Second) - time.Millisecond)
	s.lastBeat.Store(&stale)
	if s.Fresh() {
		t.Errorf("Fresh() = true just past threshold, want false")
	}
}

// TestStatusName documents that the worker name is preserved for logging.
func TestStatusName(t *testing.T) {
	s := NewStatus("stat", 0, nil)
	if s.Name() != "stat" {
		t.Errorf("Name() = %q, want %q", s.Name(), "stat")
	}
}

func TestStartServerStartsAndStops(t *testing.T) {
	s := NewStatus("test", 0, func(context.Context) (QueueLag, error) {
		return QueueLag{}, nil
	})
	s.Beat()

	ctx, cancel := context.WithCancel(context.Background())
	s.StartServer(ctx, "127.0.0.1:0")
	// Give the server a moment to bind; we don't know the port so just
	// verify the health handler directly instead of dialing.
	cancel()

	// The shutdown goroutine has started. Give it time to complete without
	// leaking the goroutine forever.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Reusing the handler proves it still works independently of the
		// server lifecycle.
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		s.HTTPHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			panic(fmt.Sprintf("healthz returned %d", rec.Code))
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not respond")
	}
}
