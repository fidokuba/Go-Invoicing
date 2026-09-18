package admin

import (
	"context"
	"log/slog"
	"time"
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
}

// NewSessionCleanupWorker constructs a worker. interval and batchSize are
// expected to already be validated by the caller (see config.Load) —
// this constructor does not re-validate them.
func NewSessionCleanupWorker(
	repository SessionRepository,
	interval time.Duration,
	batchSize int,
	logger *slog.Logger,
) *SessionCleanupWorker {
	return &SessionCleanupWorker{
		repository: repository,
		interval:   interval,
		batchSize:  batchSize,
		logger:     logger,
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
func (w *SessionCleanupWorker) runIteration(ctx context.Context) {
	for {
		deleted, err := w.repository.DeleteExpired(ctx, time.Now().UTC(), w.batchSize)
		if err != nil {
			w.logger.Error("session cleanup failed", "error", err)
			return
		}

		if deleted > 0 {
			w.logger.Info("session cleanup removed expired sessions", "count", deleted)
		}

		if deleted < int64(w.batchSize) {
			return
		}

		if ctx.Err() != nil {
			return
		}
	}
}
