package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseLimitOffset(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
		wantOK     bool
	}{
		{"defaults when absent", "", DefaultLimit, 0, true},
		{"valid custom limit and offset", "limit=10&offset=20", 10, 20, true},
		{"limit at the maximum boundary", "limit=200", 200, 0, true},
		{"limit zero rejected", "limit=0", 0, 0, false},
		{"negative limit rejected", "limit=-5", 0, 0, false},
		{"limit over the maximum rejected", "limit=201", 0, 0, false},
		{"negative offset rejected", "offset=-1", 0, 0, false},
		{"malformed limit rejected", "limit=abc", 0, 0, false},
		{"malformed offset rejected", "offset=abc", 0, 0, false},
		{"offset zero explicit is fine", "offset=0", DefaultLimit, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)

			limit, offset, ok := ParseLimitOffset(recorder, request)

			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got ok=%v (status %d, body %s)", tt.wantOK, ok, recorder.Code, recorder.Body.String())
			}

			if !ok {
				if recorder.Code != http.StatusBadRequest {
					t.Errorf("expected status %d on rejection, got %d", http.StatusBadRequest, recorder.Code)
				}
				assertErrorCode(t, recorder, CodeValidationFailed)
				return
			}

			if limit != tt.wantLimit {
				t.Errorf("expected limit %d, got %d", tt.wantLimit, limit)
			}
			if offset != tt.wantOffset {
				t.Errorf("expected offset %d, got %d", tt.wantOffset, offset)
			}
		})
	}
}

func TestParseSortOrder(t *testing.T) {
	allowed := []string{"name", "createdAt"}

	tests := []struct {
		name      string
		query     string
		wantField string
		wantOrder string
		wantOK    bool
	}{
		{"defaults when absent", "", "name", "asc", true},
		{"valid field and order", "sort=createdAt&order=desc", "createdAt", "desc", true},
		{"order is case-insensitive", "sort=name&order=DESC", "name", "desc", true},
		{"sort is case-insensitive and normalized to canonical casing", "sort=CREATEDAT&order=asc", "createdAt", "asc", true},
		{"invalid sort field rejected", "sort=bogus", "", "", false},
		{"invalid order rejected", "order=sideways", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)

			field, order, ok := ParseSortOrder(recorder, request, allowed, "name", "asc")

			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got ok=%v (status %d, body %s)", tt.wantOK, ok, recorder.Code, recorder.Body.String())
			}

			if !ok {
				if recorder.Code != http.StatusBadRequest {
					t.Errorf("expected status %d on rejection, got %d", http.StatusBadRequest, recorder.Code)
				}
				assertErrorCode(t, recorder, CodeValidationFailed)
				return
			}

			if field != tt.wantField {
				t.Errorf("expected field %q, got %q", tt.wantField, field)
			}
			if order != tt.wantOrder {
				t.Errorf("expected order %q, got %q", tt.wantOrder, order)
			}
		})
	}
}

func TestRejectUnknownQueryParams(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"no query params", "", true},
		{"only allowed params", "limit=10&sort=name", true},
		{"unknown param rejected", "limit=10&typo=oops", false},
		{"organisationId is inert on routes that never call this at all — not exercised here", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)

			got := RejectUnknownQueryParams(recorder, request, "limit", "sort")

			if got != tt.want {
				t.Fatalf("expected %v, got %v (status %d, body %s)", tt.want, got, recorder.Code, recorder.Body.String())
			}

			if !got {
				if recorder.Code != http.StatusBadRequest {
					t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
				}
				assertErrorCode(t, recorder, CodeInvalidRequest)
			}
		})
	}
}

func TestNewListResponse_EmptyItemsIsNeverNull(t *testing.T) {
	response := NewListResponse[string](nil, DefaultLimit, 0, 0)

	if response.Items == nil {
		t.Fatal("expected Items to be an empty slice, not nil")
	}
	if len(response.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(response.Items))
	}
	if response.Pagination.Total != 0 {
		t.Errorf("expected total 0, got %d", response.Pagination.Total)
	}
}
