package admin

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createTestSessionWithExpiry inserts a session row directly with an
// explicit expires_at/revoked_at, so cleanup tests can construct exact
// eligibility boundaries without going through AuthService.Login.
func createTestSessionWithExpiry(
	t *testing.T,
	db *pgxpool.Pool,
	userID uuid.UUID,
	expiresAt time.Time,
	revokedAt *time.Time,
) uuid.UUID {
	t.Helper()

	sessionID := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)

	_, err := db.Exec(
		context.Background(),
		`INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		sessionID,
		userID,
		hashSessionToken(sessionID.String()),
		createdAt,
		expiresAt,
		revokedAt,
	)
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM sessions WHERE id = $1", sessionID)
	})

	return sessionID
}

func sessionExists(t *testing.T, db *pgxpool.Pool, sessionID uuid.UUID) bool {
	t.Helper()

	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM sessions WHERE id = $1", sessionID).Scan(&count); err != nil {
		t.Fatalf("check session existence: %v", err)
	}

	return count > 0
}

func TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-none@example.com")
	now := time.Now().UTC()

	future := createTestSessionWithExpiry(t, db, userID, now.Add(time.Hour), nil)

	repository := NewPostgresSessionRepository(db)

	// DeleteExpired is deliberately not tenant-scoped (a maintenance
	// operation over the whole table — see its own doc comment), so its
	// returned count can be inflated by unrelated eligible sessions
	// genuinely created by other tests/packages running concurrently
	// against the same real database. The authoritative assertion this
	// test actually cares about — the still-valid session survives — is
	// sessionExists below, scoped to the one row this test controls; the
	// aggregate count is deliberately not asserted on here.
	if _, err := repository.DeleteExpired(ctx, now, 100); err != nil {
		t.Fatalf("delete expired: %v", err)
	}

	if !sessionExists(t, db, future) {
		t.Error("expected the still-valid session to remain")
	}
}

func TestPostgresSessionRepository_DeleteExpired_ExpiredSessionRemoved(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-expired@example.com")
	now := time.Now().UTC()

	expired := createTestSessionWithExpiry(t, db, userID, now.Add(-time.Hour), nil)

	repository := NewPostgresSessionRepository(db)

	// See TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions's
	// comment for why this asserts "at least" rather than an exact
	// count: DeleteExpired is a whole-table maintenance sweep, so
	// unrelated concurrently-created eligible sessions can only inflate
	// this number, never deflate it below what this test's own fixture
	// contributes.
	deleted, err := repository.DeleteExpired(ctx, now, 100)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}

	if deleted < 1 {
		t.Errorf("expected at least 1 deleted, got %d", deleted)
	}

	if sessionExists(t, db, expired) {
		t.Error("expected the expired session to be removed")
	}
}

// TestPostgresSessionRepository_DeleteExpired_ExactBoundaryRemoved proves
// the "<=" boundary decided in Milestone 6: a session whose expires_at is
// exactly now is already invalid for authentication (Session.IsValid uses
// now.Before(ExpiresAt), which is false when now == ExpiresAt), so it
// must also already be an eligible cleanup candidate, not one cleanup
// cycle later.
func TestPostgresSessionRepository_DeleteExpired_ExactBoundaryRemoved(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-boundary@example.com")
	now := time.Now().UTC().Truncate(time.Microsecond)

	exact := createTestSessionWithExpiry(t, db, userID, now, nil)

	repository := NewPostgresSessionRepository(db)

	// See TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions's
	// comment for why this asserts "at least" rather than an exact count.
	deleted, err := repository.DeleteExpired(ctx, now, 100)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}

	if deleted < 1 {
		t.Errorf("expected at least 1 deleted, got %d", deleted)
	}

	if sessionExists(t, db, exact) {
		t.Error("expected a session with expires_at == now to be removed")
	}
}

func TestPostgresSessionRepository_DeleteExpired_RevokedFutureSessionRemoved(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-revoked@example.com")
	now := time.Now().UTC()
	revokedAt := now

	revoked := createTestSessionWithExpiry(t, db, userID, now.Add(time.Hour), &revokedAt)

	repository := NewPostgresSessionRepository(db)

	// See TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions's
	// comment for why this asserts "at least" rather than an exact count.
	deleted, err := repository.DeleteExpired(ctx, now, 100)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}

	if deleted < 1 {
		t.Errorf("expected at least 1 deleted, got %d", deleted)
	}

	if sessionExists(t, db, revoked) {
		t.Error("expected a revoked-but-unexpired session to be removed")
	}
}

// TestPostgresSessionRepository_DeleteExpired_MixtureRemovesOnlyEligible
// exercises every combination from the milestone's own test list in one
// pass: valid, expired, revoked-but-unexpired, and revoked-and-expired,
// including two sessions belonging to the same user, to prove cleanup
// operates per-session rather than per-user.
func TestPostgresSessionRepository_DeleteExpired_MixtureRemovesOnlyEligible(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-mixture@example.com")
	otherUserID := createTestUserForSessions(t, db, organisationID, "cleanup-mixture-2@example.com")
	now := time.Now().UTC()
	revokedAt := now

	valid := createTestSessionWithExpiry(t, db, userID, now.Add(time.Hour), nil)
	expired := createTestSessionWithExpiry(t, db, userID, now.Add(-time.Hour), nil)
	revokedUnexpired := createTestSessionWithExpiry(t, db, otherUserID, now.Add(time.Hour), &revokedAt)
	revokedExpired := createTestSessionWithExpiry(t, db, otherUserID, now.Add(-time.Hour), &revokedAt)

	repository := NewPostgresSessionRepository(db)

	// See TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions's
	// comment for why this asserts "at least" rather than an exact count.
	deleted, err := repository.DeleteExpired(ctx, now, 100)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}

	if deleted < 3 {
		t.Errorf("expected at least 3 deleted, got %d", deleted)
	}

	if !sessionExists(t, db, valid) {
		t.Error("expected the still-valid session to remain")
	}

	if sessionExists(t, db, expired) {
		t.Error("expected the expired session to be removed")
	}

	if sessionExists(t, db, revokedUnexpired) {
		t.Error("expected the revoked-but-unexpired session to be removed")
	}

	if sessionExists(t, db, revokedExpired) {
		t.Error("expected the revoked-and-expired session to be removed")
	}
}

// TestPostgresSessionRepository_DeleteExpired_SameUserSessionsIndependent
// proves cleanup never removes a session merely because the same user has
// another, ineligible one — a user with two concurrent sessions (e.g. two
// devices) should lose only the one that's actually expired/revoked, and
// the remaining session must still authenticate normally afterwards.
func TestPostgresSessionRepository_DeleteExpired_SameUserSessionsIndependent(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-same-user@example.com")
	now := time.Now().UTC()

	validTokenHash := hashSessionToken("same-user-valid-token")
	validID := uuid.New()
	if _, err := db.Exec(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		validID, userID, validTokenHash, now, now.Add(time.Hour),
	); err != nil {
		t.Fatalf("create valid session: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(context.Background(), "DELETE FROM sessions WHERE id = $1", validID) })

	expired := createTestSessionWithExpiry(t, db, userID, now.Add(-time.Hour), nil)

	repository := NewPostgresSessionRepository(db)

	// See TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions's
	// comment for why this asserts "at least" rather than an exact count.
	deleted, err := repository.DeleteExpired(ctx, now, 100)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}

	if deleted < 1 {
		t.Errorf("expected at least 1 deleted, got %d", deleted)
	}

	if sessionExists(t, db, expired) {
		t.Error("expected the expired session to be removed")
	}

	got, err := repository.GetByTokenHash(ctx, validTokenHash)
	if err != nil {
		t.Fatalf("expected the same user's other session to still be readable: %v", err)
	}

	if !got.IsValid(now) {
		t.Error("expected the same user's other session to remain valid for authentication")
	}
}

func TestPostgresSessionRepository_DeleteExpired_BatchLimitRespected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-batch@example.com")
	now := time.Now().UTC()

	const total = 5
	const limit = 2

	for i := 0; i < total; i++ {
		createTestSessionWithExpiry(t, db, userID, now.Add(-time.Hour), nil)
	}

	repository := NewPostgresSessionRepository(db)

	deleted, err := repository.DeleteExpired(ctx, now, limit)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}

	if deleted != limit {
		t.Errorf("expected exactly %d deleted (the batch limit), got %d", limit, deleted)
	}

	var remaining int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", userID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining sessions: %v", err)
	}

	if remaining != total-limit {
		t.Errorf("expected %d sessions remaining, got %d", total-limit, remaining)
	}
}

// TestPostgresSessionRepository_DeleteExpired_DrainsBacklogAcrossCalls
// proves a backlog larger than one batch is fully removed by repeatedly
// calling DeleteExpired — the behaviour SessionCleanupWorker's
// runIteration relies on to drain a backlog within one worker tick.
func TestPostgresSessionRepository_DeleteExpired_DrainsBacklogAcrossCalls(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-drain@example.com")
	now := time.Now().UTC()

	const total = 7
	const limit = 3

	for i := 0; i < total; i++ {
		createTestSessionWithExpiry(t, db, userID, now.Add(-time.Hour), nil)
	}

	repository := NewPostgresSessionRepository(db)

	totalDeleted := int64(0)
	calls := 0
	for {
		deleted, err := repository.DeleteExpired(ctx, now, limit)
		if err != nil {
			t.Fatalf("delete expired: %v", err)
		}

		totalDeleted += deleted
		calls++

		if deleted < limit {
			break
		}

		if calls > total {
			t.Fatal("DeleteExpired did not converge — backlog never drained")
		}
	}

	// See TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions's
	// comment for why this asserts "at least" rather than an exact count:
	// the loop above also drains any unrelated eligible sessions created
	// concurrently by other tests/packages, which can only add to
	// totalDeleted, never take away from what this test's own backlog
	// contributes.
	if totalDeleted < total {
		t.Errorf("expected at least %d sessions deleted across all calls, got %d", total, totalDeleted)
	}

	if calls < 2 {
		t.Errorf("expected draining a backlog of %d with batch size %d to take multiple calls, took %d", total, limit, calls)
	}

	var remaining int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", userID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining sessions: %v", err)
	}

	if remaining != 0 {
		t.Errorf("expected 0 sessions remaining after draining the backlog, got %d", remaining)
	}
}

// TestPostgresSessionRepository_DeleteExpired_TwoConcurrentWorkersConverge
// is Milestone 6's multi-instance correctness proof (section 18): two
// goroutines, standing in for two API instances each running their own
// SessionCleanupWorker, repeatedly call DeleteExpired against the same
// backlog at the same time. Overlapping candidate selection is expected
// and harmless — a row already deleted by the other goroutine's
// committed DELETE simply no longer matches the second statement's WHERE
// clause, so it's silently skipped rather than erroring or double-
// counting. This proves plain per-statement atomicity is enough: no
// SKIP LOCKED, advisory lock or leader election is needed for
// correctness, only for throughput (out of scope here).
func TestPostgresSessionRepository_DeleteExpired_TwoConcurrentWorkersConverge(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "cleanup-concurrent@example.com")
	now := time.Now().UTC()

	const total = 40
	const limit = 5

	for i := 0; i < total; i++ {
		createTestSessionWithExpiry(t, db, userID, now.Add(-time.Hour), nil)
	}

	repository := NewPostgresSessionRepository(db)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var totalDeleted int64
	var errs []error

	worker := func() {
		defer wg.Done()
		for {
			deleted, err := repository.DeleteExpired(ctx, now, limit)

			mu.Lock()
			if err != nil {
				errs = append(errs, err)
				mu.Unlock()
				return
			}
			totalDeleted += deleted
			mu.Unlock()

			if deleted == 0 {
				return
			}
		}
	}

	wg.Add(2)
	go worker()
	go worker()
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("expected no errors from concurrent cleanup, got %v", errs)
	}

	// See TestPostgresSessionRepository_DeleteExpired_NoEligibleSessions's
	// comment for why this asserts "at least" rather than an exact count.
	if totalDeleted < total {
		t.Errorf("expected at least %d total deletions across both workers, got %d", total, totalDeleted)
	}

	var remaining int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", userID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining sessions: %v", err)
	}

	if remaining != 0 {
		t.Errorf("expected the backlog to be fully drained by the two workers combined, got %d remaining", remaining)
	}
}
