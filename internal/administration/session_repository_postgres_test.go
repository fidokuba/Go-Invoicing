package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createTestUserForSessions inserts a user directly via the user
// repository (not via UserService, to avoid pulling password-hashing
// concerns into session-focused tests) and registers cleanup for it.
func createTestUserForSessions(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID, email string) uuid.UUID {
	t.Helper()

	repository := NewPostgresUserRepository(db)
	user := newTestUser(organisationID, email)

	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatalf("create test user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	return user.ID
}

func TestPostgresSessionRepository_Create(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "session-create@example.com")

	repository := NewPostgresSessionRepository(db)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	session := &Session{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashSessionToken("raw-token-for-create-test"),
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(SessionTTL),
	}

	if err := repository.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM sessions WHERE id = $1", session.ID)
	})

	got, err := repository.GetByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("get session by token hash: %v", err)
	}

	if got.ID != session.ID {
		t.Errorf("expected ID %v, got %v", session.ID, got.ID)
	}

	if got.UserID != userID {
		t.Errorf("expected user ID %v, got %v", userID, got.UserID)
	}

	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("expected CreatedAt %v, got %v", createdAt, got.CreatedAt)
	}

	if !got.ExpiresAt.Equal(createdAt.Add(SessionTTL)) {
		t.Errorf("expected ExpiresAt %v, got %v", createdAt.Add(SessionTTL), got.ExpiresAt)
	}

	if got.RevokedAt != nil {
		t.Errorf("expected RevokedAt to be nil for a new session, got %v", *got.RevokedAt)
	}
}

// TestPostgresSessionRepository_Create_StoresHashNotRawToken proves the
// raw token is never what's persisted or queryable: looking a session up
// by the raw value fails, while looking it up by the value's SHA-256 hash
// succeeds.
func TestPostgresSessionRepository_Create_StoresHashNotRawToken(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "raw-token-test@example.com")

	repository := NewPostgresSessionRepository(db)

	const rawToken = "this-is-the-raw-token-nobody-should-store"
	createdAt := time.Now().UTC()
	session := &Session{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashSessionToken(rawToken),
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(SessionTTL),
	}

	if err := repository.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM sessions WHERE id = $1", session.ID)
	})

	if _, err := repository.GetByTokenHash(ctx, rawToken); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected looking up by the raw token to fail with ErrSessionNotFound, got %v", err)
	}

	if _, err := repository.GetByTokenHash(ctx, hashSessionToken(rawToken)); err != nil {
		t.Fatalf("expected looking up by the token's hash to succeed, got %v", err)
	}

	var storedTokenHash string
	if err := db.QueryRow(ctx, "SELECT token_hash FROM sessions WHERE id = $1", session.ID).Scan(&storedTokenHash); err != nil {
		t.Fatalf("read stored token_hash: %v", err)
	}

	if storedTokenHash == rawToken {
		t.Fatal("expected the stored token_hash column to differ from the raw token")
	}
}

func TestPostgresSessionRepository_GetByTokenHash_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresSessionRepository(db)

	got, err := repository.GetByTokenHash(ctx, hashSessionToken("no-such-token"))
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}

	if got != nil {
		t.Errorf("expected nil session, got %+v", got)
	}
}

// TestPostgresSessionRepository_GetByTokenHash_ExpiredSessionIsNotValid
// proves that an expired session is still returned by the repository
// (this lookup does not filter) but is reported invalid by
// Session.IsValid — validity is a domain concern, not a query filter.
func TestPostgresSessionRepository_GetByTokenHash_ExpiredSessionIsNotValid(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "expired-session@example.com")

	repository := NewPostgresSessionRepository(db)

	createdAt := time.Now().UTC().Add(-SessionTTL - time.Hour)
	session := &Session{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashSessionToken("expired-session-token"),
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(SessionTTL), // already in the past
	}

	if err := repository.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM sessions WHERE id = $1", session.ID)
	})

	got, err := repository.GetByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("get session by token hash: %v", err)
	}

	if got.IsValid(time.Now().UTC()) {
		t.Error("expected an expired session to be reported invalid by IsValid")
	}
}

// TestPostgresSessionRepository_GetByTokenHash_RevokedSessionIsNotValid
// mirrors the expired case for revocation. Nothing in this milestone sets
// revoked_at (logout/revocation is out of scope for Part 2), so this test
// sets it directly via SQL — proving the schema and IsValid are already
// ready for that future feature.
func TestPostgresSessionRepository_GetByTokenHash_RevokedSessionIsNotValid(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	userID := createTestUserForSessions(t, db, organisationID, "revoked-session@example.com")

	repository := NewPostgresSessionRepository(db)

	createdAt := time.Now().UTC()
	session := &Session{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashSessionToken("revoked-session-token"),
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(SessionTTL), // not expired
	}

	if err := repository.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM sessions WHERE id = $1", session.ID)
	})

	if _, err := db.Exec(ctx, "UPDATE sessions SET revoked_at = NOW() WHERE id = $1", session.ID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	got, err := repository.GetByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("get session by token hash: %v", err)
	}

	if got.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set")
	}

	if got.IsValid(time.Now().UTC()) {
		t.Error("expected a revoked session to be reported invalid by IsValid, even though it has not expired")
	}
}
