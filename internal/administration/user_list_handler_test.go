package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestUserHandler_List_QueryValidation is a table-driven sweep of every
// query-parameter validation rule GET /users must enforce (Milestone 8
// Part 3 section 26). Authorization (admin/manager only) is enforced by
// route-level RequireRole in app.go, not this handler — see
// TestApp_UsersList_RoleMatrix for that proof; these tests call the
// handler directly, matching this file's existing convention.
func TestUserHandler_List_QueryValidation(t *testing.T) {
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
		{"invalid role rejected", "?role=superadmin", http.StatusBadRequest},
		{"valid role accepted", "?role=admin", http.StatusOK},
		{"invalid active rejected", "?active=maybe", http.StatusBadRequest},
		{"valid active accepted", "?active=true", http.StatusOK},
		{"unknown query parameter rejected", "?typo=1", http.StatusBadRequest},
		{"organisationId is unknown here and is rejected, not silently ignored", "?organisationId=" + uuid.NewString(), http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, organisationID := newTestUserHandler()

			request := httptest.NewRequest(http.MethodGet, "/users"+tt.query, nil)
			request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
			recorder := httptest.NewRecorder()

			handler.List(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestUserHandler_List_EmptyResult proves a valid query with no matches
// returns 200 with an empty items array, never 404.
func TestUserHandler_List_EmptyResult(t *testing.T) {
	handler, _, organisationID := newTestUserHandler()

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
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

// TestUserHandler_List_NeverExposesPasswordHash proves the list response
// never includes credential fields, the same guarantee GetByID already
// has, just checked at the raw-body level for the list endpoint too.
func TestUserHandler_List_NeverExposesPasswordHash(t *testing.T) {
	handler, users, organisationID := newTestUserHandler()

	u := newTestUser(organisationID, "listed@example.com")
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request = withAuthenticatedIdentity(request, newIdentity(organisationID, UserRoleAdmin))
	recorder := httptest.NewRecorder()

	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	body := strings.ToLower(recorder.Body.String())
	if strings.Contains(body, "passwordhash") || strings.Contains(body, "password_hash") {
		t.Errorf("expected no password hash field in list response, got: %s", recorder.Body.String())
	}
}
