package customer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestCustomerHandler_List_QueryValidation is a table-driven sweep of
// every query-parameter validation rule GET /customers must enforce
// (Milestone 8 Part 3 section 26).
func TestCustomerHandler_List_QueryValidation(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"defaults", "", http.StatusOK},
		{"valid custom limit and offset", "?limit=10&offset=5", http.StatusOK},
		{"limit zero rejected", "?limit=0", http.StatusBadRequest},
		{"negative limit rejected", "?limit=-1", http.StatusBadRequest},
		{"limit over 200 rejected", "?limit=500", http.StatusBadRequest},
		{"negative offset rejected", "?offset=-5", http.StatusBadRequest},
		{"malformed limit rejected", "?limit=x", http.StatusBadRequest},
		{"invalid sort field rejected", "?sort=bogus", http.StatusBadRequest},
		{"invalid order rejected", "?order=up", http.StatusBadRequest},
		{"invalid status rejected", "?status=deleted", http.StatusBadRequest},
		{"valid status accepted", "?status=active", http.StatusOK},
		{"empty search is absent, not an error", "?search=", http.StatusOK},
		{"unknown query parameter rejected", "?typo=1", http.StatusBadRequest},
		{"organisationId is unknown here and is rejected, not silently ignored", "?organisationId=" + uuid.NewString(), http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler()

			request := httptest.NewRequest(http.MethodGet, "/customers"+tt.query, nil)
			request = withAuthenticatedOrganisation(request, uuid.New())
			recorder := httptest.NewRecorder()

			handler.List(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestCustomerHandler_List_EmptyResult proves a valid query with no
// matches returns 200 with an empty items array, never 404.
func TestCustomerHandler_List_EmptyResult(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodGet, "/customers", nil)
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response struct {
		Items      []json.RawMessage `json:"items"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Items == nil {
		t.Error("expected items to be an empty array, not null")
	}
	if response.Pagination.Total != 0 {
		t.Errorf("expected total 0, got %d", response.Pagination.Total)
	}
}

// TestCustomerHandler_List_TenantIsolation proves a customer created
// under one organisation never appears in a different organisation's
// list.
func TestCustomerHandler_List_TenantIsolation(t *testing.T) {
	handler, repository, _ := newTestHandlerWithFakes()

	orgA := uuid.New()
	orgB := uuid.New()
	customerID := uuid.New()
	repository.customers[customerID] = Customer{ID: customerID, OrganisationID: orgA, Name: "Org A Customer", Status: CustomerStatusActive}

	request := httptest.NewRequest(http.MethodGet, "/customers", nil)
	request = withAuthenticatedOrganisation(request, orgB)
	recorder := httptest.NewRecorder()

	handler.List(recorder, request)

	var response struct {
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Pagination.Total != 0 {
		t.Errorf("expected 0 customers visible to a different organisation, got %d", response.Pagination.Total)
	}
}
