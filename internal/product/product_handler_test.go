package product

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

func newTestHandler() *ProductHandler {
	service := NewProductService(newFakeProductRepository())
	return NewProductHandler(service)
}

// withAuthenticatedOrganisation attaches an AuthenticatedUser identity
// scoped to organisationID to r, the way AuthMiddleware.RequireAuth would
// have — these tests invoke the handler directly, bypassing the
// middleware, so they must set up the same context it would have.
func withAuthenticatedOrganisation(r *http.Request, organisationID uuid.UUID) *http.Request {
	identity := admin.AuthenticatedUser{UserID: uuid.New(), OrganisationID: organisationID, Role: admin.UserRoleUser}
	return r.WithContext(admin.WithAuthenticatedUser(r.Context(), identity))
}

func TestProductHandler_Create(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response ProductResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Name != "Widget" {
		t.Errorf("expected name %q, got %q", "Widget", response.Name)
	}

	if response.SKU != "SKU-1" {
		t.Errorf("expected SKU %q, got %q", "SKU-1", response.SKU)
	}

	if response.Price != 1999 {
		t.Errorf("expected price 1999, got %d", response.Price)
	}

	if response.OrganisationID != organisationID.String() {
		t.Errorf("expected organisation ID %q, got %q", organisationID.String(), response.OrganisationID)
	}

	if _, err := uuid.Parse(response.ID); err != nil {
		t.Errorf("expected response ID to be a valid UUID, got %q", response.ID)
	}
}

// TestProductHandler_Create_IgnoresOrganisationIdQueryParameter is the
// crux Milestone 4 Part 4 regression test: a client supplying
// ?organisationId=<some other organisation> must have zero effect — the
// created product must belong to the authenticated organisation, not the
// one named in the query string.
func TestProductHandler_Create_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	handler := newTestHandler()
	authenticatedOrganisationID := uuid.New()
	otherOrganisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+otherOrganisationID.String(), body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, authenticatedOrganisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response ProductResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.OrganisationID != authenticatedOrganisationID.String() {
		t.Errorf("expected the product to belong to the authenticated organisation %q, got %q (organisationId query parameter must be ignored)", authenticatedOrganisationID.String(), response.OrganisationID)
	}
}

func TestProductHandler_Create_MissingName(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"   ","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_Create_MissingSKU(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"   ","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_Create_NegativePrice(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":-1}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_Create_InvalidJSON(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_Create_DuplicateSKU(t *testing.T) {
	repository := newFakeProductRepository()
	repository.createErr = ErrProductSKUAlreadyExists
	service := NewProductService(repository)
	handler := NewProductHandler(service)
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_Create_UnexpectedRepositoryError(t *testing.T) {
	repository := newFakeProductRepository()
	repository.createErr = errors.New("connection reset by peer")
	service := NewProductService(repository)
	handler := NewProductHandler(service)
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
}

// TestProductHandler_Create_MissingAuthenticatedContext proves Create
// fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth — replacing the old
// TestProductHandler_Create_MissingOrganisationID (400), since there is
// no longer an organisationId query parameter to be missing.
func TestProductHandler_Create_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_GetByID(t *testing.T) {
	repository := newFakeProductRepository()
	service := NewProductService(repository)
	handler := NewProductHandler(service)

	organisationID := uuid.New()
	p, err := service.Create(context.Background(), organisationID, "Widget", "", "SKU-1", 1999, "")
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/products/"+p.ID.String(), nil)
	request.SetPathValue("id", p.ID.String())
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response ProductResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != p.ID.String() {
		t.Errorf("expected ID %q, got %q", p.ID.String(), response.ID)
	}
}

// TestProductHandler_GetByID_IgnoresOrganisationIdQueryParameter proves a
// client cannot use ?organisationId=<other> to reach into another
// organisation's product — the authenticated context alone decides
// tenant scope, so this must behave exactly as if the query parameter
// were absent (i.e. still 404 for another organisation's product).
func TestProductHandler_GetByID_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	repository := newFakeProductRepository()
	service := NewProductService(repository)
	handler := NewProductHandler(service)

	ownerOrganisationID := uuid.New()
	attackerOrganisationID := uuid.New()

	p, err := service.Create(context.Background(), ownerOrganisationID, "Widget", "", "SKU-1", 1999, "")
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	// Authenticated as attackerOrganisationID, but the query string names
	// the real owner — if the query parameter had any effect, this would
	// wrongly succeed.
	request := httptest.NewRequest(http.MethodGet, "/products/"+p.ID.String()+"?organisationId="+ownerOrganisationID.String(), nil)
	request.SetPathValue("id", p.ID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_GetByID_InvalidUUID(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/products/not-a-uuid", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestProductHandler_GetByID_MissingAuthenticatedContext proves GetByID
// fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth — replacing the old
// TestProductHandler_GetByID_MissingOrganisationID (400).
func TestProductHandler_GetByID_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()
	id := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/products/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_GetByID_NotFound(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()
	id := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/products/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_GetByID_WrongOrganisation(t *testing.T) {
	repository := newFakeProductRepository()
	service := NewProductService(repository)
	handler := NewProductHandler(service)

	organisationA := uuid.New()
	organisationB := uuid.New()

	p, err := service.Create(context.Background(), organisationA, "Widget", "", "SKU-1", 1999, "")
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/products/"+p.ID.String(), nil)
	request.SetPathValue("id", p.ID.String())
	request = withAuthenticatedOrganisation(request, organisationB)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
