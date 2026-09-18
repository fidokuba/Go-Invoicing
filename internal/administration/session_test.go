package admin

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSession_IsValid_NotExpiredNotRevoked(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	session := &Session{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
	}

	if !session.IsValid(now) {
		t.Error("expected a freshly created session to be valid")
	}
}

func TestSession_IsValid_Expired(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	session := &Session{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		CreatedAt: now.Add(-SessionTTL - time.Minute),
		ExpiresAt: now.Add(-time.Minute),
	}

	if session.IsValid(now) {
		t.Error("expected an expired session to be invalid")
	}
}

func TestSession_IsValid_ExactlyAtExpiry(t *testing.T) {
	// ExpiresAt itself is not valid: IsValid uses now.Before(ExpiresAt),
	// so the boundary instant itself is already expired.
	expiresAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	session := &Session{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		CreatedAt: expiresAt.Add(-SessionTTL),
		ExpiresAt: expiresAt,
	}

	if session.IsValid(expiresAt) {
		t.Error("expected a session to be invalid at the exact instant it expires")
	}
}

func TestSession_IsValid_Revoked(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-time.Minute)

	session := &Session{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(SessionTTL), // not expired
		RevokedAt: &revokedAt,
	}

	if session.IsValid(now) {
		t.Error("expected a revoked session to be invalid even though it has not expired")
	}
}
