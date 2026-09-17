package invoice

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func newTestHandler(f *testFixture) *InvoiceHandler {
	return NewInvoiceHandler(f.service)
}

func TestInvoiceHandler_Create(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	body := bytes.NewBufferString(`{
		"customerId": "` + f.customerID.String() + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": [
			{"description": "Consulting", "quantity": 1.5, "unitPrice": 1000, "vatRate": 20}
		]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices?organisationId="+f.organisationID.String(), body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response InvoiceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.CustomerID != f.customerID.String() {
		t.Errorf("expected customer ID %q, got %q", f.customerID.String(), response.CustomerID)
	}

	if len(response.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(response.Lines))
	}

	// quantity 1.5 * unitPrice 1000 = 1500 subtotal; vat 20% = 300; total 1800.
	if response.Subtotal != 1500 || response.VATTotal != 300 || response.Total != 1800 {
		t.Errorf("expected subtotal=1500 vatTotal=300 total=1800, got subtotal=%d vatTotal=%d total=%d",
			response.Subtotal, response.VATTotal, response.Total)
	}

	if _, err := uuid.Parse(response.ID); err != nil {
		t.Errorf("expected response ID to be a valid UUID, got %q", response.ID)
	}
}

func TestInvoiceHandler_Create_InvalidJSON(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/invoices?organisationId="+f.organisationID.String(), body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Create_ValidationFailure(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	// No lines: a validation failure, not a not-found or server error.
	body := bytes.NewBufferString(`{
		"customerId": "` + f.customerID.String() + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": []
	}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices?organisationId="+f.organisationID.String(), body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Create_CustomerNotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	body := bytes.NewBufferString(`{
		"customerId": "` + uuid.New().String() + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": [
			{"description": "Consulting", "quantity": 1, "unitPrice": 1000, "vatRate": 20}
		]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices?organisationId="+f.organisationID.String(), body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Create_MissingOrganisationID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	body := bytes.NewBufferString(`{
		"customerId": "` + f.customerID.String() + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": [
			{"description": "Consulting", "quantity": 1, "unitPrice": 1000, "vatRate": 20}
		]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices", body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetByID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := validRequest(f.customerID)
	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String()+"?organisationId="+f.organisationID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, httpRequest)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response InvoiceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != created.ID.String() {
		t.Errorf("expected ID %q, got %q", created.ID.String(), response.ID)
	}

	if len(response.Lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(response.Lines))
	}
}

func TestInvoiceHandler_GetByID_InvalidUUID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := httptest.NewRequest(http.MethodGet, "/invoices/not-a-uuid?organisationId="+f.organisationID.String(), nil)
	request.SetPathValue("id", "not-a-uuid")
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetByID_MissingOrganisationID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/invoices/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetByID_NotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/invoices/"+id.String()+"?organisationId="+f.organisationID.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetByID_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := validRequest(f.customerID)
	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	otherOrgID := uuid.New()
	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String()+"?organisationId="+otherOrgID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, httpRequest)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
