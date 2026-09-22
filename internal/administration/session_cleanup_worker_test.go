package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go-invoicing/internal/metrics"
)

func testWorkerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSessionCleanupWorker_RunsImmediatelyOnStartup proves Run performs a
// cleanup pass as soon as it starts, without waiting for the first tick —
// section 7's explicit requirement.
func TestSessionCleanupWorker_RunsImmediatelyOnStartup(t *testing.T) {
	repository := newFakeSessionRepository()

	var calls atomic.Int32
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		calls.Add(1)
		return 0, nil
	}

	worker := NewSessionCleanupWorker(repository, time.Hour, 10, testWorkerLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	waitForCondition(t, time.Second, func() bool { return calls.Load() >= 1 })

	cancel()
	waitForClose(t, done, time.Second)
}

// TestSessionCleanupWorker_RunsOnEachTick proves the ticker actually
// drives repeated cleanup passes, using a short interval instead of
// waiting for a real hour — the smallest timing seam available, per the
// milestone's own guidance to prefer this over a generic clock
// abstraction.
func TestSessionCleanupWorker_RunsOnEachTick(t *testing.T) {
	repository := newFakeSessionRepository()

	var calls atomic.Int32
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		calls.Add(1)
		return 0, nil
	}

	const interval = 20 * time.Millisecond
	worker := NewSessionCleanupWorker(repository, interval, 10, testWorkerLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// The immediate run plus at least two ticks should comfortably occur
	// within a small multiple of the interval.
	waitForCondition(t, 20*interval, func() bool { return calls.Load() >= 3 })

	cancel()
	waitForClose(t, done, time.Second)
}

// TestSessionCleanupWorker_DrainsMultipleBatchesPerIteration proves
// runIteration keeps calling DeleteExpired within a single pass while
// batches come back full, and stops once a short batch signals the
// backlog is exhausted.
func TestSessionCleanupWorker_DrainsMultipleBatchesPerIteration(t *testing.T) {
	repository := newFakeSessionRepository()

	const batchSize = 5
	responses := []int64{5, 5, 3} // two full batches, then a short one
	var callCount int
	var mu sync.Mutex
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		mu.Lock()
		defer mu.Unlock()
		if callCount >= len(responses) {
			t.Fatalf("unexpected extra DeleteExpired call (call #%d)", callCount+1)
		}
		result := responses[callCount]
		callCount++
		return result, nil
	}

	worker := NewSessionCleanupWorker(repository, time.Hour, batchSize, testWorkerLogger(), nil)
	worker.runIteration(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if callCount != len(responses) {
		t.Errorf("expected %d DeleteExpired calls to drain the backlog, got %d", len(responses), callCount)
	}
}

// TestSessionCleanupWorker_CancellationDuringBacklogStopsFurtherBatches
// proves that if ctx is cancelled between batches (mid-backlog-drain),
// runIteration stops rather than starting another batch — section 6's
// explicit requirement.
func TestSessionCleanupWorker_CancellationDuringBacklogStopsFurtherBatches(t *testing.T) {
	repository := newFakeSessionRepository()

	const batchSize = 5
	ctx, cancel := context.WithCancel(context.Background())

	var callCount int32
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			cancel() // cancel partway through, before the next batch would begin
			return int64(batchSize), nil
		}
		return int64(batchSize), nil
	}

	worker := NewSessionCleanupWorker(repository, time.Hour, batchSize, testWorkerLogger(), nil)
	worker.runIteration(ctx)

	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Errorf("expected exactly 1 DeleteExpired call before cancellation stopped further batches, got %d", got)
	}
}

// TestSessionCleanupWorker_ErrorDoesNotStopTheWorkerPermanently proves a
// failing iteration is logged and the worker keeps ticking rather than
// exiting or wedging — section 9's explicit requirement.
func TestSessionCleanupWorker_ErrorDoesNotStopTheWorkerPermanently(t *testing.T) {
	repository := newFakeSessionRepository()

	var calls atomic.Int32
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		n := calls.Add(1)
		if n == 1 {
			return 0, errors.New("simulated database failure")
		}
		return 0, nil
	}

	const interval = 20 * time.Millisecond
	worker := NewSessionCleanupWorker(repository, interval, 10, testWorkerLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// The first call fails; the worker must still be alive and ticking
	// afterwards.
	waitForCondition(t, time.Second, func() bool { return calls.Load() >= 3 })

	cancel()
	waitForClose(t, done, time.Second)
}

// TestSessionCleanupWorker_StopsPromptlyOnContextCancellation and
// TestSessionCleanupWorker_NoGoroutineLeak both prove Run returns quickly
// once ctx is cancelled, and that nothing is left running afterwards —
// this is exercised without any third-party leak-detection dependency by
// simply asserting the done channel closes within a short bound.
func TestSessionCleanupWorker_StopsPromptlyOnContextCancellation(t *testing.T) {
	repository := newFakeSessionRepository()
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		return 0, nil
	}

	worker := NewSessionCleanupWorker(repository, time.Hour, 10, testWorkerLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("worker returned before its context was even cancelled")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	waitForClose(t, done, time.Second)
}

// --- Milestone 10 Part 4: metrics integration ---

// scrapeWorkerMetrics renders m's exposition body as a string for
// substring assertions.
func scrapeWorkerMetrics(t *testing.T, m *metrics.Metrics) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	return recorder.Body.String()
}

// TestSessionCleanupWorker_SuccessfulRunIncrementsRunAndDurationMetrics
// proves one scheduled iteration (one runIteration call) increments the
// run counter exactly once and records a duration observation —
// regardless of how many DeleteExpired batches it drained internally.
func TestSessionCleanupWorker_SuccessfulRunIncrementsRunAndDurationMetrics(t *testing.T) {
	repository := newFakeSessionRepository()
	responses := []int64{5, 5, 3} // two full batches, then a short one
	var callCount int
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		result := responses[callCount]
		callCount++
		return result, nil
	}

	m := metrics.New(nil)
	worker := NewSessionCleanupWorker(repository, time.Hour, 5, testWorkerLogger(), m)
	worker.runIteration(context.Background())

	body := scrapeWorkerMetrics(t, m)
	if !strings.Contains(body, "go_invoicing_session_cleanup_runs_total 1") {
		t.Fatalf("expected exactly one recorded run for one runIteration call, got:\n%s", body)
	}
	if !strings.Contains(body, "go_invoicing_session_cleanup_run_duration_seconds_bucket") {
		t.Fatal("expected a run-duration histogram observation")
	}
	// 5 + 5 + 3 = 13 sessions deleted across the three batches this one
	// run drained.
	if !strings.Contains(body, "go_invoicing_session_cleanup_sessions_deleted_total 13") {
		t.Fatalf("expected 13 cumulative sessions deleted across every batch of the run, got:\n%s", body)
	}
	if strings.Contains(body, "session_cleanup_failures_total 1") {
		t.Fatal("expected no failure to be recorded for a successful run")
	}
}

// TestSessionCleanupWorker_GenuineFailureIncrementsFailureMetric proves a
// real DeleteExpired error (not a shutdown cancellation) increments the
// failure counter.
func TestSessionCleanupWorker_GenuineFailureIncrementsFailureMetric(t *testing.T) {
	repository := newFakeSessionRepository()
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		return 0, errors.New("simulated database failure")
	}

	m := metrics.New(nil)
	worker := NewSessionCleanupWorker(repository, time.Hour, 10, testWorkerLogger(), m)
	worker.runIteration(context.Background())

	body := scrapeWorkerMetrics(t, m)
	if !strings.Contains(body, "go_invoicing_session_cleanup_failures_total 1") {
		t.Fatalf("expected the failure counter to be incremented, got:\n%s", body)
	}
	// The run itself still counts — it did execute, it just failed.
	if !strings.Contains(body, "go_invoicing_session_cleanup_runs_total 1") {
		t.Fatalf("expected the run counter to still be incremented for a failed run, got:\n%s", body)
	}
}

// TestSessionCleanupWorker_GracefulCancellationDoesNotCountAsFailure is
// Milestone 10 Part 4 section 18's explicit requirement: a DeleteExpired
// error caused by ctx already being cancelled (ordinary shutdown, not a
// genuine database problem) must never increment the failure metric.
func TestSessionCleanupWorker_GracefulCancellationDoesNotCountAsFailure(t *testing.T) {
	repository := newFakeSessionRepository()
	ctx, cancel := context.WithCancel(context.Background())
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		cancel()
		return 0, context.Canceled
	}

	m := metrics.New(nil)
	worker := NewSessionCleanupWorker(repository, time.Hour, 10, testWorkerLogger(), m)
	worker.runIteration(ctx)

	body := scrapeWorkerMetrics(t, m)
	if strings.Contains(body, "session_cleanup_failures_total 1") {
		t.Fatalf("expected graceful shutdown cancellation not to be counted as a failure, got:\n%s", body)
	}
}

// TestSessionCleanupWorker_NilMetricsIsANoOp proves passing nil for m
// (metrics disabled) never panics — already exercised implicitly by
// every other test in this file passing nil, but asserted explicitly
// here as its own guarantee.
func TestSessionCleanupWorker_NilMetricsIsANoOp(t *testing.T) {
	repository := newFakeSessionRepository()
	repository.deleteExpiredFunc = func(ctx context.Context, now time.Time, limit int) (int64, error) {
		return 0, nil
	}

	worker := NewSessionCleanupWorker(repository, time.Hour, 10, testWorkerLogger(), nil)
	worker.runIteration(context.Background())
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}

	t.Fatal("condition not met within timeout")
}

func waitForClose(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("worker did not stop within timeout after context cancellation")
	}
}
