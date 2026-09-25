package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// QueueLag is a point-in-time snapshot of one index type's backlog.
type QueueLag struct {
	PendingCount         int64
	OldestPendingSeconds int64
}

// LagFunc returns the current queue lag for one index type. It is injected
// into Status so the metrics collector and the heartbeat server can be unit
// tested without a real database.
type LagFunc func(context.Context) (QueueLag, error)

// Status records a worker's heartbeat and serves /healthz and /metrics.
// Metrics are exposed on a dedicated registry, never the Prometheus
// default registry, so worker binaries stay self-contained.
type Status struct {
	name         string
	pollInterval time.Duration
	lagFunc      LagFunc
	lastBeat     atomic.Pointer[time.Time]
	registry     *prometheus.Registry

	pendingDesc   *prometheus.Desc
	oldestDesc    *prometheus.Desc
	heartbeatDesc *prometheus.Desc
}

var _ prometheus.Collector = (*Status)(nil)

// NewStatus creates a Status for the named worker. The poll interval is used
// to compute freshness (see Fresh). lagFunc is called on every metrics scrape.
func NewStatus(name string, pollInterval time.Duration, lagFunc LagFunc) *Status {
	s := &Status{
		name:         name,
		pollInterval: pollInterval,
		lagFunc:      lagFunc,
		registry:     prometheus.NewRegistry(),
		pendingDesc: prometheus.NewDesc(
			"indexer_queue_pending",
			"Number of pending jobs in the index queue.",
			nil, nil,
		),
		oldestDesc: prometheus.NewDesc(
			"indexer_queue_oldest_pending_seconds",
			"Age in seconds of the oldest pending job in the index queue.",
			nil, nil,
		),
		heartbeatDesc: prometheus.NewDesc(
			"indexer_last_heartbeat_timestamp_seconds",
			"Unix timestamp of the last worker heartbeat.",
			nil, nil,
		),
	}
	s.registry.MustRegister(s)
	return s
}

// Name returns the worker name that appears in logs.
func (s *Status) Name() string { return s.name }

// Beat records a heartbeat at the current time. It is safe for concurrent use.
func (s *Status) Beat() {
	now := time.Now()
	s.lastBeat.Store(&now)
}

// Fresh reports whether the worker is recently alive. A worker is fresh if it
// has heartbeated at least once and the most recent heartbeat is within
// 2*PollInterval + 10s.
func (s *Status) Fresh() bool {
	last := s.lastBeat.Load()
	if last == nil {
		return false
	}
	return time.Since(*last) <= 2*s.pollInterval+10*time.Second
}

// HTTPHandler returns the worker's health/metrics HTTP handler.
func (s *Status) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{
		Registry: s.registry,
	}))
	return mux
}

// StartServer starts an HTTP server on addr in a background goroutine. The
// server is shut down when ctx is cancelled.
func (s *Status) StartServer(ctx context.Context, addr string) {
	srv := &http.Server{
		Addr:     addr,
		Handler:  s.HTTPHandler(),
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("health server exited", "index", s.name, "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("health server shutdown failed", "index", s.name, "error", err)
		}
	}()
}

func (s *Status) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.Fresh() {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "ok")
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, "stale")
}

// Describe implements prometheus.Collector.
func (s *Status) Describe(ch chan<- *prometheus.Desc) {
	ch <- s.pendingDesc
	ch <- s.oldestDesc
	ch <- s.heartbeatDesc
}

// Collect implements prometheus.Collector. It queries the current lag and
// emits the pending/oldest metrics along with the heartbeat gauge. If the lag
// query fails, the error is logged and only the heartbeat gauge is exposed.
func (s *Status) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lag, err := s.lagFunc(ctx)
	if err != nil {
		slog.Error("lag query failed", "index", s.name, "error", err)
	} else {
		ch <- prometheus.MustNewConstMetric(
			s.pendingDesc,
			prometheus.GaugeValue,
			float64(lag.PendingCount),
		)
		ch <- prometheus.MustNewConstMetric(
			s.oldestDesc,
			prometheus.GaugeValue,
			float64(lag.OldestPendingSeconds),
		)
	}

	ch <- prometheus.MustNewConstMetric(
		s.heartbeatDesc,
		prometheus.GaugeValue,
		s.lastBeatTimestamp(),
	)
}

func (s *Status) lastBeatTimestamp() float64 {
	last := s.lastBeat.Load()
	if last == nil {
		return 0
	}
	return float64(last.Unix())
}
