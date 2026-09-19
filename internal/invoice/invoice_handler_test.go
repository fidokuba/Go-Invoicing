package invoice

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

func newTestHandler(f *testFixture) *InvoiceHandler {
	return NewInvoiceHandler(f.service, f.pdfService())
}

// withAuthenticatedOrganisation attaches an AuthenticatedUser identity
// scoped to organisationID to r, the way AuthMiddleware.RequireAuth would
// have — these tests invoke the handler directly, bypassing the
// middleware, so they must set up the same context it would have.
func withAuthenticatedOrganisation(r *http.Request, organisationID uuid.UUID) *http.Request {
	identity := admin.AuthenticatedUser{UserID: uuid.New(), OrganisationID: organisationID, Role: admin.UserRoleUser}
	return r.WithContext(admin.WithAuthenticatedUser(r.Context(), identity))
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
	request := httptest.NewRequest(http.MethodPost, "/invoices", body)
	request = withAuthenticatedOrganisation(request, f.organisationID)
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

	// A brand-new invoice has no payments: amountPaid is 0 and the full
	// total is outstanding.
	if response.AmountPaid != 0 {
		t.Errorf("expected amountPaid 0, got %d", response.AmountPaid)
	}

	if response.AmountOutstanding != response.Total {
		t.Errorf("expected amountOutstanding to equal total (%d), got %d", response.Total, response.AmountOutstanding)
	}

	if _, err := uuid.Parse(response.ID); err != nil {
		t.Errorf("expected response ID to be a valid UUID, got %q", response.ID)
	}
}

// TestInvoiceHandler_Create_IgnoresOrganisationIdQueryParameter is the
// crux Milestone 4 Part 4 regression test: a client supplying
// ?organisationId=<some other organisation> must have zero effect — the
// created invoice must belong to the authenticated organisation, not the
// one named in the query string. f.customerID only exists under
// f.organisationID, so if the query parameter were mistakenly honoured
// this would fail with "customer not found" instead of succeeding.
func TestInvoiceHandler_Create_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	otherOrganisationID := uuid.New()

	body := bytes.NewBufferString(`{
		"customerId": "` + f.customerID.String() + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": [
			{"description": "Consulting", "quantity": 1, "unitPrice": 1000, "vatRate": 20}
		]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/invoices?organisationId="+otherOrganisationID.String(), body)
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response InvoiceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.OrganisationID != f.organisationID.String() {
		t.Errorf("expected the invoice to belong to the authenticated organisation %q, got %q", f.organisationID.String(), response.OrganisationID)
	}
}

func TestInvoiceHandler_Create_InvalidJSON(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/invoices", body)
	request = withAuthenticatedOrganisation(request, f.organisationID)
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
	request := httptest.NewRequest(http.MethodPost, "/invoices", body)
	request = withAuthenticatedOrganisation(request, f.organisationID)
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
	request := httptest.NewRequest(http.MethodPost, "/invoices", body)
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_Create_MissingAuthenticatedContext proves Create
// fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth — replacing the old
// TestInvoiceHandler_Create_MissingOrganisationID (400), since there is
// no longer an organisationId query parameter to be missing.
func TestInvoiceHandler_Create_MissingAuthenticatedContext(t *testing.T) {
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

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
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

	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	httpRequest = withAuthenticatedOrganisation(httpRequest, f.organisationID)
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

	// No payments have been made against this invoice.
	if response.AmountPaid != 0 {
		t.Errorf("expected amountPaid 0, got %d", response.AmountPaid)
	}

	if response.AmountOutstanding != response.Total {
		t.Errorf("expected amountOutstanding to equal total (%d), got %d", response.Total, response.AmountOutstanding)
	}
}

func TestInvoiceHandler_GetByID_WithPayments(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := validRequest(f.customerID)
	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	// A newly-created invoice is Draft and cannot accept a payment — send
	// it first.
	if _, err := f.service.Send(context.Background(), f.organisationID, created.ID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	// validRequest's default line is quantity 1, unitPrice 1000, vatRate
	// 20 -> total 1200. Pay less than that: a partial payment.
	if _, _, err := f.service.CreatePayment(context.Background(), f.organisationID, created.ID, CreatePaymentRequest{
		Amount:        500,
		PaymentMethod: "cash",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	httpRequest = withAuthenticatedOrganisation(httpRequest, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, httpRequest)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response InvoiceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.AmountPaid != 500 {
		t.Errorf("expected amountPaid 500, got %d", response.AmountPaid)
	}

	wantOutstanding := response.Total - 500
	if response.AmountOutstanding != wantOutstanding {
		t.Errorf("expected amountOutstanding %d, got %d", wantOutstanding, response.AmountOutstanding)
	}
}

func TestInvoiceHandler_GetByID_MultiplePayments(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := validRequest(f.customerID)
	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if _, err := f.service.Send(context.Background(), f.organisationID, created.ID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	for _, amount := range []int64{300, 400} {
		if _, _, err := f.service.CreatePayment(context.Background(), f.organisationID, created.ID, CreatePaymentRequest{
			Amount:        amount,
			PaymentMethod: "cash",
		}); err != nil {
			t.Fatalf("create payment of %d: %v", amount, err)
		}
	}

	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	httpRequest = withAuthenticatedOrganisation(httpRequest, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, httpRequest)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response InvoiceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	wantPaid := int64(300 + 400)
	if response.AmountPaid != wantPaid {
		t.Errorf("expected amountPaid %d, got %d", wantPaid, response.AmountPaid)
	}

	if response.AmountOutstanding != response.Total-wantPaid {
		t.Errorf("expected amountOutstanding %d, got %d", response.Total-wantPaid, response.AmountOutstanding)
	}
}

func TestInvoiceHandler_GetByID_FullyPaid_OutstandingIsZero(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := validRequest(f.customerID)
	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if _, err := f.service.Send(context.Background(), f.organisationID, created.ID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	// validRequest's default line totals 1200 — pay exactly that.
	if _, _, err := f.service.CreatePayment(context.Background(), f.organisationID, created.ID, CreatePaymentRequest{
		Amount:        1200,
		PaymentMethod: "cash",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	httpRequest = withAuthenticatedOrganisation(httpRequest, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, httpRequest)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response InvoiceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.AmountOutstanding != 0 {
		t.Errorf("expected amountOutstanding 0 for a fully paid invoice, got %d", response.AmountOutstanding)
	}

	if response.Status != InvoiceStatusPaid {
		t.Errorf("expected status %q, got %q", InvoiceStatusPaid, response.Status)
	}
}

func TestInvoiceHandler_GetByID_InvalidUUID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := httptest.NewRequest(http.MethodGet, "/invoices/not-a-uuid", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_GetByID_MissingAuthenticatedContext proves GetByID
// fails closed (401) when invoked without going through
// AuthMiddleware.RequireAuth — replacing the old
// TestInvoiceHandler_GetByID_MissingOrganisationID (400).
func TestInvoiceHandler_GetByID_MissingAuthenticatedContext(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/invoices/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetByID_NotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/invoices/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
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
	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	httpRequest = withAuthenticatedOrganisation(httpRequest, otherOrgID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, httpRequest)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_GetByID_IgnoresOrganisationIdQueryParameter proves a
// client cannot use ?organisationId=<other> to reach into another
// organisation's invoice — the authenticated context alone decides
// tenant scope.
func TestInvoiceHandler_GetByID_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := validRequest(f.customerID)
	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	attackerOrganisationID := uuid.New()

	// Authenticated as attackerOrganisationID, but the query string names
	// the real owner — if the query parameter had any effect, this would
	// wrongly succeed.
	httpRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String()+"?organisationId="+f.organisationID.String(), nil)
	httpRequest.SetPathValue("id", created.ID.String())
	httpRequest = withAuthenticatedOrganisation(httpRequest, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, httpRequest)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
