package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthHandler_Logout_MissingAuthenticatedContext proves Logout fails
// closed with 401 when invoked without going through
// AuthMiddleware.RequireAuth — the same fail-closed contract every other
// protected handler in this package already has.
func TestAuthHandler_Logout_MissingAuthenticatedContext(t *testing.T) {
	f := newAuthTestFixture(t)
	handler := NewAuthHandler(f.service)

	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestAuthHandler_Logout_Success proves a valid, authenticated logout
// returns 204 No Content with an empty body.
func TestAuthHandler_Logout_Success(t *testing.T) {
	f := newAuthTestFixture(t)
	handler := NewAuthHandler(f.service)

	result, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	identity := AuthenticatedUser{UserID: f.activeUserID, OrganisationID: f.organisationID, Role: UserRoleUser, SessionID: result.Session.ID}
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request = request.WithContext(WithAuthenticatedUser(request.Context(), identity))
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNoContent, recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("expected an empty body, got %q", recorder.Body.String())
	}

	session, err := f.sessionRepository.GetByTokenHash(context.Background(), hashSessionToken(result.Token))
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.RevokedAt == nil {
		t.Error("expected the session's RevokedAt to be populated after logout")
	}
}
