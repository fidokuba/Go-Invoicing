package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// assertForbiddenBody decodes recorder's body as the standard JSON error
// envelope and asserts it carries forbidden's fixed, generic
// {code, message} — used by every test asserting a 403, in this file and
// in user_handler_test.go, so the envelope shape only needs updating in
// one place if it ever changes.
func assertForbiddenBody(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}

	if body.Error.Code != "forbidden" || body.Error.Message != "forbidden" {
		t.Errorf("expected generic {code: forbidden, message: forbidden}, got %+v", body.Error)
	}
}

// doRoleGatedRequest builds a request, optionally attaching identity to
// its context (nil means no authenticated identity at all), and runs it
// through a RequireRole(roles...)-wrapped spy handler.
func doRoleGatedRequest(t *testing.T, roles []string, identity *AuthenticatedUser) (*httptest.ResponseRecorder, *spyHandler) {
	t.Helper()

	spy := &spyHandler{}
	middleware := RequireRole(roles...)(spy.handle)

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if identity != nil {
		request = withAuthenticatedIdentity(request, *identity)
	}
	recorder := httptest.NewRecorder()

	middleware(recorder, request)

	return recorder, spy
}

func TestRequireRole_AllowsPermittedRole(t *testing.T) {
	tests := []struct {
		name       string
		allowed    []string
		actualRole string
	}{
		{"admin allowed when admin is permitted", []string{UserRoleAdmin, UserRoleManager}, UserRoleAdmin},
		{"manager allowed when manager is permitted", []string{UserRoleAdmin, UserRoleManager}, UserRoleManager},
		{"single-role allow list", []string{UserRoleUser}, UserRoleUser},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := AuthenticatedUser{UserID: uuid.New(), OrganisationID: uuid.New(), Role: tt.actualRole}
			recorder, spy := doRoleGatedRequest(t, tt.allowed, &identity)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
			}

			if !spy.called {
				t.Error("expected the downstream handler to be called")
			}

			if !spy.hadOK || spy.identity != identity {
				t.Errorf("expected the downstream handler to receive %+v from context, got %+v (present: %v)", identity, spy.identity, spy.hadOK)
			}
		})
	}
}

func TestRequireRole_DeniesRoleNotInList(t *testing.T) {
	tests := []struct {
		name       string
		allowed    []string
		actualRole string
	}{
		{"user denied where not permitted", []string{UserRoleAdmin, UserRoleManager}, UserRoleUser},
		{"manager denied from an admin-only route", []string{UserRoleAdmin}, UserRoleManager},
		{"unknown role denied", []string{UserRoleAdmin, UserRoleManager}, "superadmin"},
		{"empty role denied", []string{UserRoleAdmin, UserRoleManager}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := AuthenticatedUser{UserID: uuid.New(), OrganisationID: uuid.New(), Role: tt.actualRole}
			recorder, spy := doRoleGatedRequest(t, tt.allowed, &identity)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusForbidden, recorder.Code, recorder.Body.String())
			}

			assertForbiddenBody(t, recorder)

			if spy.called {
				t.Error("expected the downstream handler not to be called")
			}
		})
	}
}

// TestRequireRole_MissingAuthenticatedContext proves RequireRole fails
// closed with the ordinary authentication failure (401), not a role
// failure (403), when invoked with no identity in context at all —
// authentication must be checked before authorisation, and a missing
// identity is an authentication problem, not an authorisation one.
func TestRequireRole_MissingAuthenticatedContext(t *testing.T) {
	recorder, spy := doRoleGatedRequest(t, []string{UserRoleAdmin, UserRoleManager}, nil)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestRequireRole_AllForbiddenResponsesAreIdentical(t *testing.T) {
	scenarios := []AuthenticatedUser{
		{UserID: uuid.New(), OrganisationID: uuid.New(), Role: UserRoleUser},
		{UserID: uuid.New(), OrganisationID: uuid.New(), Role: "superadmin"},
		{UserID: uuid.New(), OrganisationID: uuid.New(), Role: ""},
	}

	var referenceStatus int
	var referenceBody string

	for _, identity := range scenarios {
		recorder, _ := doRoleGatedRequest(t, []string{UserRoleAdmin}, &identity)

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("role %q: expected status %d, got %d", identity.Role, http.StatusForbidden, recorder.Code)
		}

		if referenceStatus == 0 {
			referenceStatus = recorder.Code
			referenceBody = recorder.Body.String()
			continue
		}

		if recorder.Code != referenceStatus || recorder.Body.String() != referenceBody {
			t.Errorf("role %q: expected identical response (status %d, body %q), got status %d, body %q",
				identity.Role, referenceStatus, referenceBody, recorder.Code, recorder.Body.String())
		}
	}
}
