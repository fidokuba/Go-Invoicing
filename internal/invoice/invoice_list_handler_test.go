package invoice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestInvoiceHandler_List_QueryValidation is a table-driven sweep of
// every query-parameter validation rule GET /invoices must enforce
// (Milestone 8 Part 3 section 26) — one handler, one table, rather than
// a separate test function per case.
func TestInvoiceHandler_List_QueryValidation(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"defaults", "", http.StatusOK},
		{"valid custom limit and offset", "?limit=10&offset=0", http.StatusOK},
		{"limit zero rejected", "?limit=0", http.StatusBadRequest},
		{"negative limit rejected", "?limit=-1", http.StatusBadRequest},
		{"limit over 200 rejected", "?limit=201", http.StatusBadRequest},
		{"negative offset rejected", "?offset=-1", http.StatusBadRequest},
		{"malformed limit rejected", "?limit=abc", http.StatusBadRequest},
		{"malformed offset rejected", "?offset=abc", http.StatusBadRequest},
		{"invalid sort field rejected", "?sort=bogus", http.StatusBadRequest},
		{"invalid order rejected", "?order=sideways", http.StatusBadRequest},
		{"invalid status rejected", "?status=cancelled", http.StatusBadRequest},
		{"valid status accepted", "?status=draft", http.StatusOK},
		{"malformed customerId rejected", "?customerId=not-a-uuid", http.StatusBadRequest},
		{"malformed issueDateFrom rejected", "?issueDateFrom=not-a-date", http.StatusBadRequest},
		{"malformed dueDateTo rejected", "?dueDateTo=2026-13-99", http.StatusBadRequest},
		{"reversed issue date range rejected", "?issueDateFrom=2026-06-30&issueDateTo=2026-06-01", http.StatusBadRequest},
		{"reversed due date range rejected", "?dueDateFrom=2026-06-30&dueDateTo=2026-06-01", http.StatusBadRequest},
		{"empty search is simply absent, not an error", "?search=", http.StatusOK},
		{"unknown query parameter rejected", "?bogus=1", http.StatusBadRequest},
		{"organisationId is an unknown parameter here and is rejected, not silently ignored", "?organisationId=11111111-1111-1111-1111-111111111111", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTestFixture()
			handler := newTestHandler(f)

			request := httptest.NewRequest(http.MethodGet, "/invoices"+tt.query, nil)
			request = withAuthenticatedOrganisation(request, f.organisationID)
			recorder := httptest.NewRecorder()

			handler.List(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}

			if recorder.Header().Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", recorder.Header().Get("Content-Type"))
			}
		})
	}
}

// TestInvoiceHandler_List_EmptyResult proves a valid query with no
// matches returns 200 with an empty items array and total 0 — never 404
// (Milestone 8 Part 3 section 23).
func TestInvoiceHandler_List_EmptyResult(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response struct {
		Items      []json.RawMessage `json:"items"`
		Pagination struct {
			Limit  int   `json:"limit"`
			Offset int   `json:"offset"`
			Total  int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Items == nil {
		t.Error("expected items to be an empty array, not null")
	}
	if len(response.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(response.Items))
	}
	if response.Pagination.Total != 0 {
		t.Errorf("expected total 0, got %d", response.Pagination.Total)
	}
	if response.Pagination.Limit != 50 {
		t.Errorf("expected default limit 50, got %d", response.Pagination.Limit)
	}
}

// TestInvoiceHandler_List_TenantIsolation proves an invoice belonging to
// another organisation never appears in this organisation's list.
func TestInvoiceHandler_List_TenantIsolation(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	f.addInvoice(1000, InvoiceStatusDraft)

	otherOrgRequest := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	otherOrgRequest = withAuthenticatedOrganisation(otherOrgRequest, uuid.New())
	recorder := httptest.NewRecorder()

	handler.List(recorder, otherOrgRequest)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response struct {
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Pagination.Total != 0 {
		t.Errorf("expected 0 invoices visible to a different organisation, got %d", response.Pagination.Total)
	}
}

// TestInvoiceHandler_List_UsesSummaryRepresentation is the regression
// test for this amendment: a list row must have no "lines" key at all
// (not merely an empty array — see InvoiceListItemResponse's own
// comment for why that distinction matters), while GET /invoices/{id}
// for the exact same invoice must still return its lines in full. It
// also re-proves, through the new representation, that totals/payment
// state, currency and effective status all remain correct.
func TestInvoiceHandler_List_UsesSummaryRepresentation(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	// A due date a full year out, so this test never becomes flaky as
	// real time passes — matching the same convention
	// TestApp_InvoiceLifecycle_DraftSentPaid already uses for the exact
	// same reason.
	issueDate := time.Now().UTC().Format("2006-01-02")
	farFutureDueDate := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	request := CreateInvoiceRequest{
		CustomerID: f.customerID.String(),
		IssueDate:  issueDate,
		DueDate:    farFutureDueDate,
		Lines: []CreateInvoiceLineRequest{
			{Description: "Consulting", Quantity: 2, UnitPrice: 5000, VATRate: 20},
		},
	}
	created, _, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	// Partially pay it so amountPaid/amountOutstanding are both non-zero,
	// non-derived-by-coincidence values.
	if _, err := f.service.Send(context.Background(), f.organisationID, created.ID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}
	if _, _, err := f.service.CreatePayment(context.Background(), f.organisationID, created.ID, CreatePaymentRequest{
		Amount:        4000,
		PaymentMethod: "cash",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	// --- GET /invoices (list): no "lines" key, correct summary fields ---

	listRequest := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	listRequest = withAuthenticatedOrganisation(listRequest, f.organisationID)
	listRecorder := httptest.NewRecorder()

	handler.List(listRecorder, listRequest)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, listRecorder.Code, listRecorder.Body.String())
	}

	var listResponse struct {
		Items      []map[string]json.RawMessage `json:"items"`
		Pagination struct {
			Limit  int   `json:"limit"`
			Offset int   `json:"offset"`
			Total  int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	if len(listResponse.Items) != 1 {
		t.Fatalf("expected exactly 1 item, got %d", len(listResponse.Items))
	}
	item := listResponse.Items[0]

	if _, hasLines := item["lines"]; hasLines {
		t.Error(`expected list item to have no "lines" key at all, got one`)
	}

	wantPresent := []string{
		"id", "invoiceNumber", "customerId", "issueDate", "dueDate",
		"status", "currency", "subtotal", "vatTotal", "total",
		"amountPaid", "amountOutstanding", "sentAt", "createdAt", "updatedAt",
	}
	for _, key := range wantPresent {
		if _, ok := item[key]; !ok {
			t.Errorf("expected list item to contain key %q", key)
		}
	}

	assertJSONString(t, item["id"], created.ID.String())
	assertJSONString(t, item["customerId"], f.customerID.String())
	assertJSONString(t, item["status"], "sent")
	assertJSONString(t, item["currency"], "GBP")

	var total, amountPaid, amountOutstanding int64
	_ = json.Unmarshal(item["total"], &total)
	_ = json.Unmarshal(item["amountPaid"], &amountPaid)
	_ = json.Unmarshal(item["amountOutstanding"], &amountOutstanding)

	if total != 12000 {
		t.Errorf("expected total 12000, got %d", total)
	}
	if amountPaid != 4000 {
		t.Errorf("expected amountPaid 4000, got %d", amountPaid)
	}
	if amountOutstanding != 8000 {
		t.Errorf("expected amountOutstanding 8000, got %d", amountOutstanding)
	}

	if listResponse.Pagination.Total != 1 || listResponse.Pagination.Limit != 50 || listResponse.Pagination.Offset != 0 {
		t.Errorf("expected unchanged pagination metadata {total:1 limit:50 offset:0}, got %+v", listResponse.Pagination)
	}

	// --- GET /invoices/{id}: lines are still present in full ---

	getRequest := httptest.NewRequest(http.MethodGet, "/invoices/"+created.ID.String(), nil)
	getRequest.SetPathValue("id", created.ID.String())
	getRequest = withAuthenticatedOrganisation(getRequest, f.organisationID)
	getRecorder := httptest.NewRecorder()

	handler.GetByID(getRecorder, getRequest)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, getRecorder.Code, getRecorder.Body.String())
	}

	var full InvoiceResponse
	if err := json.NewDecoder(getRecorder.Body).Decode(&full); err != nil {
		t.Fatalf("decode full invoice response: %v", err)
	}

	if len(full.Lines) != 1 {
		t.Fatalf("expected GET /invoices/{id} to still return 1 line, got %d", len(full.Lines))
	}
	if full.Lines[0].Description != "Consulting" {
		t.Errorf("expected the line's description to round-trip, got %q", full.Lines[0].Description)
	}
}

func assertJSONString(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()

	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %s as string: %v", raw, err)
	}
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
