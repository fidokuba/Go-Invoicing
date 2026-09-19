package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestRegistrationHandler() (*RegistrationHandler, *registrationTestFixture) {
	f := newRegistrationTestFixture()
	return NewRegistrationHandler(f.service), f
}

func TestRegistrationHandler_Register_Success(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response RegisterResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Organisation.Name != "Acme Ltd" {
		t.Errorf("expected organisation name %q, got %q", "Acme Ltd", response.Organisation.Name)
	}

	if response.User.Email != "alice@example.com" {
		t.Errorf("expected user email %q, got %q", "alice@example.com", response.User.Email)
	}

	if response.User.Role != UserRoleAdmin {
		t.Errorf("expected user role %q, got %q", UserRoleAdmin, response.User.Role)
	}
}

// TestRegistrationHandler_Register_ResponseContainsNoCredentialFieldsOrToken
// proves the response has no password/hash fields (UserResponse's
// existing guarantee) and, per Milestone 4 Part 5, no session token
// either — registration does not authenticate the caller.
func TestRegistrationHandler_Register_ResponseContainsNoCredentialFieldsOrToken(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	raw := strings.ToLower(recorder.Body.String())
	if strings.Contains(raw, "password") {
		t.Errorf("expected response body to contain no password-related fields, got %s", recorder.Body.String())
	}

	if strings.Contains(raw, "token") {
		t.Errorf("expected response body to contain no session token, got %s", recorder.Body.String())
	}
}

func TestRegistrationHandler_Register_MalformedJSON(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestRegistrationHandler_Register_MissingOrganisationName(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "   "},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestRegistrationHandler_Register_ShortPassword(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "short"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestRegistrationHandler_Register_InvalidEmailRejected is the
// Milestone 8 Part 2 regression test for the new user-email format
// validation (section 20), reached via Register too since both entry
// points share normalizeAndValidateUserFields.
func TestRegistrationHandler_Register_InvalidEmailRejected(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "not-an-email", "password": "` + registrationPassword + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestRegistrationHandler_Register_UnknownRoleFieldRejected proves the
// wire format has nowhere for a client to even attempt to request a
// role — RegisterUserRequest has no Role field at all — and, since
// Milestone 8 Part 2's strict decoding (DisallowUnknownFields), an
// attempt to send one is no longer silently dropped but rejected
// outright with 400. This is a deliberate behaviour change from the
// pre-Part-2 contract (see git history for the superseded
// TestRegistrationHandler_Register_NoRoleFieldAccepted, which proved the
// old silently-ignored behaviour): failing loudly on an unrecognised
// field is a stronger guarantee than silently discarding it.
func TestRegistrationHandler_Register_UnknownRoleFieldRejected(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `", "role": "user"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestRegistrationHandler_Register_InjectedFieldsRejected proves the full
// set of unexpected/attacker-controlled fields the Milestone 4 Part 6
// audit called out — an "organisationId" on both the organisation and
// user objects, and a "role" on the user object — are rejected outright
// by Milestone 8 Part 2's strict decoding (DisallowUnknownFields), rather
// than silently ignored as they were before: no organisation or user is
// created at all for a request carrying any of them.
func TestRegistrationHandler_Register_InjectedFieldsRejected(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	attackerOrganisationID := "11111111-1111-1111-1111-111111111111"

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Example", "organisationId": "` + attackerOrganisationID + `"},
		"user": {"name": "Alice", "email": "injection-test@example.com", "password": "` + registrationPassword + `", "role": "user", "organisationId": "` + attackerOrganisationID + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestRegistrationHandler_Register_DuplicateEmail(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Register(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected first registration to succeed with status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	body = bytes.NewBufferString(`{
		"organisation": {"name": "Second Co"},
		"user": {"name": "Alice Again", "email": "alice@example.com", "password": "` + registrationPassword + `"}
	}`)
	request = httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.Register(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

func TestRegistrationHandler_Register_UnexpectedFailure(t *testing.T) {
	handler, f := newTestRegistrationHandler()
	f.organisationRepository.createErr = errors.New("connection reset by peer")

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
}
