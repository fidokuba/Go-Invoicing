package admin

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go-invoicing/internal/metrics"
)

// SessionCleanupWorker periodically removes session rows that are no
// longer usable (expired or revoked) so the sessions table doesn't grow
// without bound. It is Milestone 6's only background job — see
// SessionRepository.DeleteExpired for the exact eligibility rule.
//
// This is a concrete type, not an implementation of some generic Worker
// interface: with exactly one background job in the project, an
// interface would have nothing to abstract over yet. One can be
// introduced once a second worker actually needs it.
//
// It holds no AuthenticatedUser and crosses no tenant boundary: sessions
// belong to users, not organisations (see Session's own doc comment), so
// there is no per-tenant scoping question for this job at all.
type SessionCleanupWorker struct {
	repository SessionRepository
	interval   time.Duration
	batchSize  int
	logger     *slog.Logger
	metrics    *metrics.Metrics
}

// NewSessionCleanupWorker constructs a worker. interval and batchSize are
// expected to already be validated by the caller (see config.Load) —
// this constructor does not re-validate them. m may be nil (metrics
// disabled — see config.Config.MetricsEnabled): every *metrics.Metrics
// method runIteration calls is a nil-safe no-op.
func NewSessionCleanupWorker(
	repository SessionRepository,
	interval time.Duration,
	batchSize int,
	logger *slog.Logger,
	m *metrics.Metrics,
) *SessionCleanupWorker {
	return &SessionCleanupWorker{
		repository: repository,
		interval:   interval,
		batchSize:  batchSize,
		logger:     logger,
		metrics:    m,
	}
}

// Run cleans up immediately, then again on every tick of interval, until
// ctx is cancelled. It deliberately returns nothing: a failed cleanup
// iteration is logged and simply retried on the next scheduled tick (see
// runIteration) rather than escalated to the caller, and a programming
// defect should panic normally rather than being swallowed — this method
// does not recover from panics.
func (w *SessionCleanupWorker) Run(ctx context.Context) {
	w.logger.Info("session cleanup worker started", "interval", w.interval, "batchSize", w.batchSize)
	defer w.logger.Info("session cleanup worker stopped")

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.runIteration(ctx)

	for {
		select {
		case <-ticker.C:
			w.runIteration(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// runIteration drains the current backlog of eligible sessions in bounded
// batches. Each DeleteExpired call is already an atomic, bounded SQL
// statement, so no transaction wraps the iteration itself. A batch
// smaller than batchSize means the backlog is exhausted, so the iteration
// ends; a full batch means more may remain, so it loops — but checks ctx
// between batches first, so a shutdown in progress stops the drain rather
// than starting another one. ctx is also passed straight through to
// DeleteExpired: a cleanup DELETE is maintenance work, not a transaction
// that must be allowed to outlive a shutdown request.
//
// One call to runIteration is exactly one Milestone 10 Part 4 "run" — the
// immediate pass Run performs at startup, or the pass one ticker tick
// triggers — regardless of how many DeleteExpired batches it drains
// internally below; RecordWorkerRun/RecordSessionsDeleted's own doc
// comments carry the same distinction. Metrics only ever observe this
// existing behaviour (scheduling, batch draining, and shutdown handling
// are all unchanged) — nothing here adds a retry or otherwise changes
// what runIteration already did before this milestone.
func (w *SessionCleanupWorker) runIteration(ctx context.Context) {
	start := time.Now()
	defer func() {
		w.metrics.RecordWorkerRun(time.Since(start))
	}()

	for {
		deleted, err := w.repository.DeleteExpired(ctx, time.Now().UTC(), w.batchSize)
		if err != nil {
			w.logger.Error("session cleanup failed", "error", err)
			if !isShutdownCancellation(ctx, err) {
				w.metrics.RecordWorkerFailure()
			}
			return
		}

		if deleted > 0 {
			w.logger.Info("session cleanup removed expired sessions", "count", deleted)
			w.metrics.RecordSessionsDeleted(deleted)
		}

		if deleted < int64(w.batchSize) {
			return
		}

		if ctx.Err() != nil {
			return
		}
	}
}

// isShutdownCancellation reports whether err is (or is caused by) ctx
// having already been cancelled or timed out — i.e. this is graceful
// shutdown in progress, not a genuine database/query failure. Milestone
// 10 Part 4 section 18's explicit requirement: a normal context
// cancellation during shutdown must never increment the worker failure
// metric, or every ordinary shutdown would look like an operational
// incident on a dashboard. ctx.Err() is checked first because it is the
// authoritative, synchronous signal for the very context this call was
// made with; errors.Is is also checked in case the repository's own
// error wraps context.Canceled/DeadlineExceeded without ctx.Err() having
// been re-read.
func isShutdownCancellation(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}

	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
