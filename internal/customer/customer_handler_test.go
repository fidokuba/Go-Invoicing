package customer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func newTestHandler() *CustomerHandler {
	service := NewCustomerService(newFakeCustomerRepository())
	return NewCustomerHandler(service)
}

func TestCustomerHandler_Create(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"Acme Ltd","email":"hello@acme.test"}`)
	request := httptest.NewRequest(http.MethodPost, "/customers?organisationId="+organisationID.String(), body)
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

func TestCustomerHandler_Create_MissingName(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	body := bytes.NewBufferString(`{"name":"   "}`)
	request := httptest.NewRequest(http.MethodPost, "/customers?organisationId="+organisationID.String(), body)
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
	request := httptest.NewRequest(http.MethodPost, "/customers?organisationId="+organisationID.String(), body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_Create_MissingOrganisationID(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"name":"Acme Ltd"}`)
	request := httptest.NewRequest(http.MethodPost, "/customers", body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetByID(t *testing.T) {
	repository := newFakeCustomerRepository()
	service := NewCustomerService(repository)
	handler := NewCustomerHandler(service)

	organisationID := uuid.New()
	c, err := service.Create(context.Background(), organisationID, "Acme Ltd", "", "", "", "")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/customers/"+c.ID.String()+"?organisationId="+organisationID.String(), nil)
	request.SetPathValue("id", c.ID.String())
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

func TestCustomerHandler_GetByID_InvalidUUID(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/customers/not-a-uuid?organisationId="+organisationID.String(), nil)
	request.SetPathValue("id", "not-a-uuid")
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetByID_NotFound(t *testing.T) {
	handler := newTestHandler()
	organisationID := uuid.New()
	id := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/customers/"+id.String()+"?organisationId="+organisationID.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetByID_WrongOrganisation(t *testing.T) {
	repository := newFakeCustomerRepository()
	service := NewCustomerService(repository)
	handler := NewCustomerHandler(service)

	organisationA := uuid.New()
	organisationB := uuid.New()

	c, err := service.Create(context.Background(), organisationA, "Acme Ltd", "", "", "", "")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/customers/"+c.ID.String()+"?organisationId="+organisationB.String(), nil)
	request.SetPathValue("id", c.ID.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
