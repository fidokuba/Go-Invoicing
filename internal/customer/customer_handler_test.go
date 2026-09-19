package customer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

func newTestHandler() *CustomerHandler {
	service := NewCustomerService(newFakeCustomerRepository(), newFakeAddressRepository())
	return NewCustomerHandler(service)
}

// newTestHandlerWithFakes is like newTestHandler but also returns the two
// underlying fakes, for tests (billing-address ones) that need to
// register a customer/organisation relationship or a customer directly
// against the repositories rather than only through the handler.
func newTestHandlerWithFakes() (*CustomerHandler, *fakeCustomerRepository, *fakeAddressRepository) {
	customerRepository := newFakeCustomerRepository()
	addressRepository := newFakeAddressRepository()
	service := NewCustomerService(customerRepository, addressRepository)
	return NewCustomerHandler(service), customerRepository, addressRepository
}

// withAuthenticatedOrganisation attaches an AuthenticatedUser identity
// scoped to organisationID to r, the way AuthMiddleware.RequireAuth would
// have — these tests invoke the handler directly, bypassing the
// middleware, so they must set up the same context it would have.
func withAuthenticatedOrganisation(r *http.Request, organisationID uuid.UUID) *http.Request {
	identity := admin.AuthenticatedUser{UserID: uuid.New(), OrganisationID: organisationID, Role: admin.UserRoleUser}
	return r.WithContext(admin.WithAuthenticatedUser(r.Context(), identity))
}

func TestCustomerHandler_Create(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Acme Ltd","email":"hello@acme.test"}`)
	request := httptest.NewRequest(http.MethodPost, "/customers", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response CustomerResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Name != "Acme Ltd" {
		t.Errorf("expected name %q, got %q", "Acme Ltd", response.Name)
	}

	if response.OrganisationID != organisationID.String() {
		t.Errorf("expected organisation ID %q, got %q", organisationID.String(), response.OrganisationID)
	}

	if _, err := uuid.Parse(response.ID); err != nil {
		t.Errorf("expected response ID to be a valid UUID, got %q", response.ID)
	}
}

// TestCustomerHandler_Create_IgnoresOrganisationIdQueryParameter is the
// crux Milestone 4 Part 4 regression test: a client supplying
// ?organisationId=<some other organisation> must have zero effect — the
// created customer must belong to the authenticated organisation, not the
// one named in the query string.
func TestCustomerHandler_Create_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	handler := newTestHandler()
	authenticatedOrganisationID := uuid.New()
	otherOrganisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Acme Ltd"}`)
	request := httptest.NewRequest(http.MethodPost, "/customers?organisationId="+otherOrganisationID.String(), body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, authenticatedOrganisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response CustomerResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.OrganisationID != authenticatedOrganisationID.String() {
		t.Errorf("expected the customer to belong to the authenticated organisation %q, got %q (organisationId query parameter must be ignored)", authenticatedOrganisationID.String(), response.OrganisationID)
	}
}

func TestCustomerHandler_Create_MissingName(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"   "}`)
	request := httptest.NewRequest(http.MethodPost, "/customers", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_Create_InvalidJSON(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/customers", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestCustomerHandler_Create_MissingAuthenticatedContext proves Create
// fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth — replacing the old
// TestCustomerHandler_Create_MissingOrganisationID (400), since there is
// no longer an organisationId query parameter to be missing.
func TestCustomerHandler_Create_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"name":"Acme Ltd"}`)
	request := httptest.NewRequest(http.MethodPost, "/customers", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetByID(t *testing.T) {
	repository := newFakeCustomerRepository()
	service := NewCustomerService(repository, newFakeAddressRepository())
	handler := NewCustomerHandler(service)

	organisationID := uuid.New()
	c, err := service.Create(context.Background(), organisationID, "Acme Ltd", "", "", "", "")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/customers/"+c.ID.String(), nil)
	request.SetPathValue("id", c.ID.String())
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response CustomerResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != c.ID.String() {
		t.Errorf("expected ID %q, got %q", c.ID.String(), response.ID)
	}
}

// TestCustomerHandler_GetByID_IgnoresOrganisationIdQueryParameter proves
// a client cannot use ?organisationId=<other> to reach into another
// organisation's customer — the authenticated context alone decides
// tenant scope.
func TestCustomerHandler_GetByID_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	repository := newFakeCustomerRepository()
	service := NewCustomerService(repository, newFakeAddressRepository())
	handler := NewCustomerHandler(service)

	ownerOrganisationID := uuid.New()
	attackerOrganisationID := uuid.New()

	c, err := service.Create(context.Background(), ownerOrganisationID, "Acme Ltd", "", "", "", "")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// Authenticated as attackerOrganisationID, but the query string names
	// the real owner — if the query parameter had any effect, this would
	// wrongly succeed.
	request := httptest.NewRequest(http.MethodGet, "/customers/"+c.ID.String()+"?organisationId="+ownerOrganisationID.String(), nil)
	request.SetPathValue("id", c.ID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetByID_InvalidUUID(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/customers/not-a-uuid", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestCustomerHandler_GetByID_MissingAuthenticatedContext proves GetByID
// fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth.
func TestCustomerHandler_GetByID_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()
	id := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/customers/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetByID_NotFound(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()
	id := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/customers/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetByID_WrongOrganisation(t *testing.T) {
	repository := newFakeCustomerRepository()
	service := NewCustomerService(repository, newFakeAddressRepository())
	handler := NewCustomerHandler(service)

	organisationA := uuid.New()
	organisationB := uuid.New()

	c, err := service.Create(context.Background(), organisationA, "Acme Ltd", "", "", "", "")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/customers/"+c.ID.String(), nil)
	request.SetPathValue("id", c.ID.String())
	request = withAuthenticatedOrganisation(request, organisationB)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
