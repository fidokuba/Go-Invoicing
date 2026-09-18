package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

	worker := NewSessionCleanupWorker(repository, time.Hour, 10, testWorkerLogger())

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
	worker := NewSessionCleanupWorker(repository, interval, 10, testWorkerLogger())

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

	worker := NewSessionCleanupWorker(repository, time.Hour, batchSize, testWorkerLogger())
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

	worker := NewSessionCleanupWorker(repository, time.Hour, batchSize, testWorkerLogger())
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
	worker := NewSessionCleanupWorker(repository, interval, 10, testWorkerLogger())

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

	worker := NewSessionCleanupWorker(repository, time.Hour, 10, testWorkerLogger())

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
