package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestAuthService_Logout_RevokesOnlyTheGivenSession proves Logout
// revokes exactly the one session it's given, leaving a second session
// for the same user completely untouched — the core Milestone 8 Part 3
// guarantee: logging out one device must never affect another.
func TestAuthService_Logout_RevokesOnlyTheGivenSession(t *testing.T) {
	f := newAuthTestFixture(t)

	first, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	if err := f.service.Logout(context.Background(), first.Session.ID); err != nil {
		t.Fatalf("logout: %v", err)
	}

	firstSession, err := f.sessionRepository.GetByTokenHash(context.Background(), hashSessionToken(first.Token))
	if err != nil {
		t.Fatalf("get first session: %v", err)
	}
	if firstSession.RevokedAt == nil {
		t.Error("expected the first session's RevokedAt to be populated")
	}

	secondSession, err := f.sessionRepository.GetByTokenHash(context.Background(), hashSessionToken(second.Token))
	if err != nil {
		t.Fatalf("get second session: %v", err)
	}
	if secondSession.RevokedAt != nil {
		t.Error("expected the second session to remain unrevoked")
	}
}

// TestAuthService_Logout_UnknownSessionID proves Logout surfaces
// ErrSessionNotFound rather than silently succeeding for a session ID
// that doesn't exist — defensive, since in normal request flow this
// value always comes from a session AuthMiddleware just validated.
func TestAuthService_Logout_UnknownSessionID(t *testing.T) {
	f := newAuthTestFixture(t)

	err := f.service.Logout(context.Background(), uuid.New())
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}
