package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// newTestUserHandler wires a UserHandler against in-memory fakes, with a
// single organisation pre-seeded so tests can create users against it
// without going through OrganisationService.Create.
func newTestUserHandler() (*UserHandler, *fakeUserRepository, uuid.UUID) {
	organisationID := uuid.New()

	organisations := newFakeOrganisationRepository()
	organisations.organisations[organisationID] = Organisation{ID: organisationID, Name: "Test Organisation"}

	users := newFakeUserRepository()
	service := NewUserService(users, organisations)
	handler := NewUserHandler(service)

	return handler, users, organisationID
}

func newIdentity(organisationID uuid.UUID, role string) AuthenticatedUser {
	return AuthenticatedUser{UserID: uuid.New(), OrganisationID: organisationID, Role: role}
}

// --- Create (Milestone 4 Part 5: protected, role-gated at the service
// level regardless of route wiring — see UserService.Create) ---

func TestUserHandler_Create_Success(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice Example","email":"alice@example.com","password":"` + validPassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response UserResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Email != "alice@example.com" {
		t.Errorf("expected email %q, got %q", "alice@example.com", response.Email)
	}

	if response.Role != UserRoleUser {
		t.Errorf("expected default role %q, got %q", UserRoleUser, response.Role)
	}

	if response.OrganisationID != organisationID.String() {
		t.Errorf("expected organisation ID %q, got %q", organisationID.String(), response.OrganisationID)
	}

	if _, err := uuid.Parse(response.ID); err != nil {
		t.Errorf("expected response ID to be a valid UUID, got %q", response.ID)
	}
}

// TestUserHandler_Create_IgnoresOrganisationIdQueryParameter is the
// crux Milestone 4 Part 5 regression test: after removing the bootstrap
// exception, there is no client-controlled organisationId anywhere in
// the normal API — a lingering query parameter must have zero effect.
func TestUserHandler_Create_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()
	otherOrganisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/users?organisationId="+otherOrganisationID.String(), body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response UserResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.OrganisationID != organisationID.String() {
		t.Errorf("expected the user to belong to the authenticated organisation %q, got %q (organisationId query parameter must be ignored)", organisationID.String(), response.OrganisationID)
	}
}

func TestUserHandler_Create_ResponseContainsNoCredentialFields(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	raw := recorder.Body.String()
	if strings.Contains(strings.ToLower(raw), "password") {
		t.Errorf("expected response body to contain no password-related fields, got %s", raw)
	}
}

func TestUserHandler_Create_MalformedJSON(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestUserHandler_Create_MissingAuthenticatedContext proves Create fails
// closed (401) when invoked without going through
// AuthMiddleware.RequireAuth — this is now the only way "no organisation"
// can happen on this endpoint, since the bootstrap organisationId query
// parameter no longer exists at all.
func TestUserHandler_Create_MissingAuthenticatedContext(t *testing.T) {
	handler, _, _ := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestUserHandler_Create_ValidationError(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	// Password shorter than the 12-character minimum triggers a service
	// validation error, which the handler must map to 400.
	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"short"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestUserHandler_Create_OrganisationNotFound(t *testing.T) {
	handler, _, _ := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	// A random organisation ID: authenticated, but that organisation
	// doesn't exist in the fake repository — an edge case of a stale
	// identity outliving its organisation.
	request = withAuthenticatedIdentity(request, newIdentity(uuid.New(), UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestUserHandler_Create_DuplicateEmail(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()
	identity := newIdentity(organisationID, UserRoleAdmin)

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, identity)
	recorder := httptest.NewRecorder()
	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected first create to succeed with status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	body = bytes.NewBufferString(`{"name":"Alice Again","email":"alice@example.com","password":"` + validPassword + `"}`)
	request = httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, identity)
	recorder = httptest.NewRecorder()
	handler.Create(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

func TestUserHandler_Create_ManagerCreatesUser_Succeeds(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `","role":"user"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleManager))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}
}

// TestUserHandler_Create_ManagerCannotCreateManager and
// TestUserHandler_Create_ManagerCannotCreateAdmin prove the
// privilege-escalation rule reaches all the way through the handler:
// UserService.Create's rejection (ErrUserRoleAssignmentNotPermitted)
// must map to 403 forbidden, not a validation 400 or a silent downgrade.
func TestUserHandler_Create_ManagerCannotCreateManager(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `","role":"manager"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleManager))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	if recorder.Body.String() != "forbidden\n" {
		t.Errorf("expected generic body %q, got %q", "forbidden\n", recorder.Body.String())
	}

	if len(users.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

func TestUserHandler_Create_ManagerCannotCreateAdmin(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `","role":"admin"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleManager))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	if len(users.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

// TestUserHandler_Create_UserActorCannotCreateAnyone proves the
// defense-in-depth rule holds even calling the handler directly, with no
// RequireRole route gate in front of it at all (that gate is applied in
// app.go, not here).
func TestUserHandler_Create_UserActorCannotCreateAnyone(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	body := bytes.NewBufferString(`{"name":"Alice","email":"alice@example.com","password":"` + validPassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleUser))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	if len(users.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

// --- GetByID (Milestone 4 Part 4 tenant scoping + Milestone 4 Part 5
// self-vs-other authorisation) ---

func TestUserHandler_GetByID_Self_Success(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	identity := newIdentity(organisationID, UserRoleUser)
	self := &User{
		ID:             identity.UserID,
		OrganisationID: organisationID,
		Name:           "Alice",
		Email:          "alice@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	if err := users.Create(context.Background(), self); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/users/"+self.ID.String(), nil)
	request.SetPathValue("id", self.ID.String())
	request = withAuthenticatedIdentity(request, identity)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response UserResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != self.ID.String() {
		t.Errorf("expected ID %q, got %q", self.ID.String(), response.ID)
	}
}

// TestUserHandler_GetByID_User_CannotGetAnother proves a plain "user"
// role is forbidden from retrieving a colleague's record in the same
// organisation — a same-tenant authorisation failure (403), not a
// tenant-scoping one (404).
func TestUserHandler_GetByID_User_CannotGetAnother(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	other := &User{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Bob",
		Email:          "bob@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	if err := users.Create(context.Background(), other); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/users/"+other.ID.String(), nil)
	request.SetPathValue("id", other.ID.String())
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleUser))
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	if recorder.Body.String() != "forbidden\n" {
		t.Errorf("expected generic body %q, got %q", "forbidden\n", recorder.Body.String())
	}
}

func TestUserHandler_GetByID_Manager_CanGetAnother(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	other := &User{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Bob",
		Email:          "bob@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	if err := users.Create(context.Background(), other); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/users/"+other.ID.String(), nil)
	request.SetPathValue("id", other.ID.String())
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleManager))
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}
}

func TestUserHandler_GetByID_Admin_CanGetAnother(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	other := &User{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Bob",
		Email:          "bob@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	if err := users.Create(context.Background(), other); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/users/"+other.ID.String(), nil)
	request.SetPathValue("id", other.ID.String())
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}
}

// TestUserHandler_GetByID_IgnoresOrganisationIdQueryParameter proves a
// client cannot use ?organisationId=<other> to reach into another
// organisation's user — the authenticated context alone decides tenant
// scope. Uses an admin identity so the self-vs-other check (not the
// subject of this test) doesn't interfere.
func TestUserHandler_GetByID_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()
	attackerOrganisationID := uuid.New()

	user := &User{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Alice",
		Email:          "alice@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	if err := users.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Authenticated (as admin, in a different organisation) with the
	// query string naming the real owner — if the query parameter had
	// any effect, this would wrongly succeed.
	request := httptest.NewRequest(http.MethodGet, "/users/"+user.ID.String()+"?organisationId="+organisationID.String(), nil)
	request.SetPathValue("id", user.ID.String())
	request = withAuthenticatedIdentity(request, newIdentity(attackerOrganisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// TestUserHandler_GetByID_WrongOrganisation proves a user from
// Organisation A requesting a user belonging to Organisation B receives
// 404, never a hint the user exists elsewhere. Uses an admin identity so
// the self-vs-other check doesn't pre-empt the tenant-scoping one.
func TestUserHandler_GetByID_WrongOrganisation(t *testing.T) {
	handler, users, organisationA := newTestUserHandler()
	organisationB := uuid.New()

	user := &User{
		ID:             uuid.New(),
		OrganisationID: organisationB,
		Name:           "Bob",
		Email:          "bob@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	if err := users.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/users/"+user.ID.String(), nil)
	request.SetPathValue("id", user.ID.String())
	request = withAuthenticatedIdentity(request, newIdentity(organisationA, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestUserHandler_GetByID_ResponseContainsNoCredentialFields(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	identity := newIdentity(organisationID, UserRoleUser)
	self := &User{
		ID:             identity.UserID,
		OrganisationID: organisationID,
		Name:           "Alice",
		Email:          "alice@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	if err := users.Create(context.Background(), self); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/users/"+self.ID.String(), nil)
	request.SetPathValue("id", self.ID.String())
	request = withAuthenticatedIdentity(request, identity)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	raw := recorder.Body.String()
	if strings.Contains(strings.ToLower(raw), "password") {
		t.Errorf("expected response body to contain no password-related fields, got %s", raw)
	}
}

func TestUserHandler_GetByID_NotFound(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/users/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestUserHandler_GetByID_InvalidUUID(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	request := httptest.NewRequest(http.MethodGet, "/users/not-a-uuid", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestUserHandler_GetByID_MissingAuthenticatedContext proves GetByID
// fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth.
func TestUserHandler_GetByID_MissingAuthenticatedContext(t *testing.T) {
	handler, _, _ := newTestUserHandler()

	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/users/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}
