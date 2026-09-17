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
)

func newTestHandler() *ProductHandler {
	service := NewProductService(newFakeProductRepository())
	return NewProductHandler(service)
}

func TestProductHandler_Create(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+organisationID.String(), body)
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

func TestProductHandler_Create_MissingName(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"   ","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+organisationID.String(), body)
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
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+organisationID.String(), body)
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
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+organisationID.String(), body)
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
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+organisationID.String(), body)
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
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+organisationID.String(), body)
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
	request := httptest.NewRequest(http.MethodPost, "/products?organisationId="+organisationID.String(), body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_Create_MissingOrganisationID(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"name":"Widget","sku":"SKU-1","price":1999}`)
	request := httptest.NewRequest(http.MethodPost, "/products", body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
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

	request := httptest.NewRequest(http.MethodGet, "/products/"+p.ID.String()+"?organisationId="+organisationID.String(), nil)
	request.SetPathValue("id", p.ID.String())
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

func TestProductHandler_GetByID_InvalidUUID(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/products/not-a-uuid?organisationId="+organisationID.String(), nil)
	request.SetPathValue("id", "not-a-uuid")
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_GetByID_MissingOrganisationID(t *testing.T) {
	handler := newTestHandler()
	id := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/products/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestProductHandler_GetByID_NotFound(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()
	id := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/products/"+id.String()+"?organisationId="+organisationID.String(), nil)
	request.SetPathValue("id", id.String())
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

	request := httptest.NewRequest(http.MethodGet, "/products/"+p.ID.String()+"?organisationId="+organisationB.String(), nil)
	request.SetPathValue("id", p.ID.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
