package invoice

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// newPaymentTestRequest builds a request authenticated as organisationID
// (via withAuthenticatedOrganisation, defined in invoice_handler_test.go)
// rather than an organisationId query parameter — these handlers no
// longer read one (Milestone 4 Part 4).
func newPaymentTestRequest(method, url string, body *bytes.Buffer, invoiceID uuid.UUID, organisationID uuid.UUID) *http.Request {
	if body == nil {
		body = &bytes.Buffer{}
	}

	request := httptest.NewRequest(method, url, body)
	request.Header.Set("Content-Type", "application/json")
	// Every POST carries a fresh, valid Idempotency-Key (Milestone 13
	// Part 1) unless a test deliberately replaces or removes it.
	if method == http.MethodPost {
		request.Header.Set("Idempotency-Key", newTestIdempotencyKey())
	}
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, organisationID)

	return request
}

// --- POST /invoices/{id}/payments ---

func TestInvoiceHandler_CreatePayment(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{
		"amount": 5000,
		"paymentMethod": "bank_transfer",
		"paymentDate": "2026-09-17",
		"reference": "PAY-123",
		"notes": "Payment received"
	}`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response PaymentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.InvoiceID != invoiceID.String() {
		t.Errorf("expected invoice ID %q, got %q", invoiceID.String(), response.InvoiceID)
	}

	if response.Amount != 5000 {
		t.Errorf("expected amount 5000, got %d", response.Amount)
	}

	if response.PaymentMethod != "bank_transfer" {
		t.Errorf("expected payment method %q, got %q", "bank_transfer", response.PaymentMethod)
	}

	if response.PaymentDate != "2026-09-17" {
		t.Errorf("expected payment date %q, got %q", "2026-09-17", response.PaymentDate)
	}

	if response.Reference == nil || *response.Reference != "PAY-123" {
		t.Errorf("expected reference %q, got %v", "PAY-123", response.Reference)
	}

	if response.Notes == nil || *response.Notes != "Payment received" {
		t.Errorf("expected notes %q, got %v", "Payment received", response.Notes)
	}

	if _, err := uuid.Parse(response.ID); err != nil {
		t.Errorf("expected response ID to be a valid UUID, got %q", response.ID)
	}
}

func TestInvoiceHandler_CreatePayment_WithoutOptionalFields(t *testing.T) {
	// reference and notes are optional; a request omitting them must still
	// succeed, and the response should simply omit them (they're
	// `omitempty` pointers, nil when not supplied).
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{"amount": 5000, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response PaymentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Reference != nil {
		t.Errorf("expected no reference, got %v", *response.Reference)
	}

	if response.Notes != nil {
		t.Errorf("expected no notes, got %v", *response.Notes)
	}
}

func TestInvoiceHandler_CreatePayment_InvalidJSON(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_CreatePayment_InvalidUUID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	body := bytes.NewBufferString(`{"amount": 5000, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices/not-a-uuid/payments", body)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_CreatePayment_MissingAuthenticatedContext proves
// CreatePayment fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth — replacing the old
// TestInvoiceHandler_CreatePayment_MissingOrganisationID (400).
func TestInvoiceHandler_CreatePayment_MissingAuthenticatedContext(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{"amount": 5000, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", invoiceID.String())
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_CreatePayment_IgnoresOrganisationIdQueryParameter is
// the crux Milestone 4 Part 4 regression test for payment creation: a
// client authenticated as one organisation cannot use
// ?organisationId=<the invoice's real owner> to create a payment against
// an invoice belonging to a different organisation.
func TestInvoiceHandler_CreatePayment_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	attackerOrganisationID := uuid.New()

	body := bytes.NewBufferString(`{"amount": 5000, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments?organisationId="+f.organisationID.String(), body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", newTestIdempotencyKey())
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_CreatePayment_InvalidPaymentDate(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{"amount": 5000, "paymentMethod": "cash", "paymentDate": "not-a-date"}`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_CreatePayment_InvalidAmount(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{"amount": 0, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_CreatePayment_Overpayment proves the Milestone 8
// Part 2 400-vs-409 convention: this request is well-formed (a positive
// amount) but conflicts with the invoice's current outstanding balance —
// a state-dependent conflict, not a malformed request, so 409 rather
// than 400 (see ErrPaymentExceedsOutstanding's own comment).
func TestInvoiceHandler_CreatePayment_Overpayment(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{"amount": 10001, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_CreatePayment_InvoiceNotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := uuid.New()

	body := bytes.NewBufferString(`{"amount": 5000, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_CreatePayment_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	body := bytes.NewBufferString(`{"amount": 5000, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", body, invoiceID, uuid.New())
	recorder := httptest.NewRecorder()

	handler.CreatePayment(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// --- GET /invoices/{id}/payments ---

func TestInvoiceHandler_GetPayments(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	createBody := bytes.NewBufferString(`{"amount": 4000, "paymentMethod": "cash", "paymentDate": "2026-09-17"}`)
	createRequest := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", createBody, invoiceID, f.organisationID)
	createRecorder := httptest.NewRecorder()
	handler.CreatePayment(createRecorder, createRequest)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("setup: create payment failed with status %d (body: %s)", createRecorder.Code, createRecorder.Body.String())
	}

	request := newPaymentTestRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/payments", nil, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response []PaymentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response) != 1 {
		t.Fatalf("expected 1 payment, got %d", len(response))
	}

	if response[0].Amount != 4000 {
		t.Errorf("expected amount 4000, got %d", response[0].Amount)
	}
}

func TestInvoiceHandler_GetPayments_Empty(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := newPaymentTestRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/payments", nil, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response []PaymentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response) != 0 {
		t.Errorf("expected 0 payments, got %d", len(response))
	}
}

func TestInvoiceHandler_GetPayments_NotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := uuid.New()

	request := newPaymentTestRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/payments", nil, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetPayments_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := newPaymentTestRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/payments", nil, invoiceID, uuid.New())
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetPayments_InvalidUUID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := httptest.NewRequest(http.MethodGet, "/invoices/not-a-uuid/payments", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_GetPayments_MissingAuthenticatedContext proves
// GetPayments fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth.
func TestInvoiceHandler_GetPayments_MissingAuthenticatedContext(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/payments", nil)
	request.SetPathValue("id", invoiceID.String())
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_GetPayments_IgnoresOrganisationIdQueryParameter is
// the crux Milestone 4 Part 4 regression test for payment retrieval: a
// client authenticated as one organisation cannot use
// ?organisationId=<the invoice's real owner> to list payments for an
// invoice belonging to a different organisation.
func TestInvoiceHandler_GetPayments_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	attackerOrganisationID := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/payments?organisationId="+f.organisationID.String(), nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetPayments_MultiplePayments_PreservesOrder(t *testing.T) {
	// fakePaymentRepository.GetByInvoiceID returns payments in whatever
	// order they were appended (it does not sort) — the real
	// date-then-created_at ordering is proven separately, against actual
	// PostgreSQL, in TestPostgresPaymentRepository_GetByInvoiceID_
	// OrderedByDateThenCreatedAt. This test only proves the handler
	// itself doesn't reorder whatever the service/repository hands back.
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	amounts := []string{`{"amount": 1000, "paymentMethod": "cash", "paymentDate": "2026-01-01"}`,
		`{"amount": 2000, "paymentMethod": "cash", "paymentDate": "2026-02-01"}`,
		`{"amount": 3000, "paymentMethod": "cash", "paymentDate": "2026-03-01"}`,
	}

	for _, body := range amounts {
		createRequest := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", bytes.NewBufferString(body), invoiceID, f.organisationID)
		createRecorder := httptest.NewRecorder()
		handler.CreatePayment(createRecorder, createRequest)

		if createRecorder.Code != http.StatusCreated {
			t.Fatalf("setup: create payment failed with status %d (body: %s)", createRecorder.Code, createRecorder.Body.String())
		}
	}

	request := newPaymentTestRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/payments", nil, invoiceID, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPayments(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response []PaymentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response) != 3 {
		t.Fatalf("expected 3 payments, got %d", len(response))
	}

	wantOrder := []int64{1000, 2000, 3000}
	for i, want := range wantOrder {
		if response[i].Amount != want {
			t.Errorf("position %d: expected amount %d, got %d", i, want, response[i].Amount)
		}
	}
}
