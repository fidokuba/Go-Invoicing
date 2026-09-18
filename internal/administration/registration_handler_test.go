package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
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
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestRegistrationHandler_Register_NoRoleFieldAccepted proves the wire
// format has nowhere for a client to even attempt to request a role: an
// unrecognised "role" field in the user object is simply ignored by JSON
// decoding into RegisterUserRequest (which has no Role field at all),
// and the created user's role is still always admin.
func TestRegistrationHandler_Register_NoRoleFieldAccepted(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `", "role": "user"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response RegisterResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.User.Role != UserRoleAdmin {
		t.Errorf("expected the attempted \"role\":\"user\" field to be ignored and the user created as %q, got %q", UserRoleAdmin, response.User.Role)
	}
}

// TestRegistrationHandler_Register_InjectedFieldsHaveNoInfluence proves
// the full set of unexpected/attacker-controlled fields the Milestone 4
// Part 6 audit called out — an "organisationId" on both the organisation
// and user objects, and a "role" on the user object — are all silently
// ignored by Go's default json.Decoder (no DisallowUnknownFields is used
// anywhere in this codebase): registration still creates a
// server-generated organisation, the first user belongs to that
// generated organisation (never the injected ID), and the user's role
// remains admin regardless of what was requested.
func TestRegistrationHandler_Register_InjectedFieldsHaveNoInfluence(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	attackerOrganisationID := "11111111-1111-1111-1111-111111111111"

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Example", "organisationId": "` + attackerOrganisationID + `"},
		"user": {"name": "Alice", "email": "injection-test@example.com", "password": "` + registrationPassword + `", "role": "user", "organisationId": "` + attackerOrganisationID + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response RegisterResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Organisation.ID == attackerOrganisationID {
		t.Errorf("expected a server-generated organisation ID, got the injected value %q", attackerOrganisationID)
	}

	if _, err := uuid.Parse(response.Organisation.ID); err != nil {
		t.Errorf("expected a valid server-generated organisation ID, got %q", response.Organisation.ID)
	}

	if response.User.OrganisationID != response.Organisation.ID {
		t.Errorf("expected the user to belong to the generated organisation %q, got %q", response.Organisation.ID, response.User.OrganisationID)
	}

	if response.User.Role != UserRoleAdmin {
		t.Errorf("expected the injected \"role\":\"user\" to be ignored and the user created as %q, got %q", UserRoleAdmin, response.User.Role)
	}
}

func TestRegistrationHandler_Register_DuplicateEmail(t *testing.T) {
	handler, _ := newTestRegistrationHandler()

	body := bytes.NewBufferString(`{
		"organisation": {"name": "Acme Ltd"},
		"user": {"name": "Alice", "email": "alice@example.com", "password": "` + registrationPassword + `"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
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
	recorder := httptest.NewRecorder()

	handler.Register(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
}
