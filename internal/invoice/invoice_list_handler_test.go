package invoice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
