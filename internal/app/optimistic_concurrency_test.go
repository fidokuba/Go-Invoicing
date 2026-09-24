package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Milestone 13 Part 2: optimistic concurrency on /organisation and
// /organisation/settings through the real router, auth/role middleware
// and PostgreSQL.

// doRequestWithIfMatch sends a PATCH with explicit control over If-Match
// (omitted when ifMatch is empty) — doRequest itself fetches a fresh
// ETag for every PATCH to these routes.
func doRequestWithIfMatch(handler http.Handler, path, token, body, ifMatch string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	if ifMatch != "" {
		request.Header.Set("If-Match", ifMatch)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

func etagOf(t *testing.T, handler http.Handler, path, token string) string {
	t.Helper()

	recorder := doRequest(handler, http.MethodGet, path, token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d (body: %s)", path, recorder.Code, recorder.Body.String())
	}
	etag := recorder.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("GET %s: expected an ETag", path)
	}

	return etag
}

// The M13.2 audit's reproduced lost update, now rejected: two admin
// sessions load the organisation form at the same version; A saves a
// phone change; B's stale save (which would have silently reverted A's
// phone) gets 412, and A's change survives.
func TestApp_OptimisticConcurrency_StaleOrganisationFormIsRejected(t *testing.T) {
	handler, db := newTestApp(t)
	email := "occ-admin-" + uuid.NewString() + "@example.com"
	tenant := registerTenant(t, handler, db, "OCC Org", email)
	sessionA := tenant.token
	sessionB := loginAs(t, handler, email, testPassword)
	const path = "/api/v1/organisation"

	etagA := etagOf(t, handler, path, sessionA)
	etagB := etagOf(t, handler, path, sessionB)
	if etagA != etagB {
		t.Fatalf("expected both sessions to read the same version, got %s and %s", etagA, etagB)
	}

	saveA := doRequestWithIfMatch(handler, path, sessionA, `{"name":"OCC Org","email":"old@example.com","phone":"0200-NEW","website":"","taxId":""}`, etagA)
	if saveA.Code != http.StatusOK {
		t.Fatalf("A's save: expected 200, got %d (body: %s)", saveA.Code, saveA.Body.String())
	}
	if saveA.Header().Get("ETag") == etagA {
		t.Error("expected A's save to return a new ETag")
	}

	saveB := doRequestWithIfMatch(handler, path, sessionB, `{"name":"OCC Org","email":"new@example.com","phone":"","website":"","taxId":""}`, etagB)
	if saveB.Code != http.StatusPreconditionFailed || errorCode(t, saveB) != "precondition_failed" {
		t.Fatalf("B's stale save: expected 412 precondition_failed, got %d (body: %s)", saveB.Code, saveB.Body.String())
	}

	current := doRequest(handler, http.MethodGet, path, sessionA, nil)
	var organisation struct {
		Email *string `json:"email"`
		Phone *string `json:"phone"`
	}
	_ = json.NewDecoder(current.Body).Decode(&organisation)
	if organisation.Phone == nil || *organisation.Phone != "0200-NEW" {
		t.Errorf("expected A's phone change to survive, got %v", organisation.Phone)
	}
	if organisation.Email == nil || *organisation.Email != "old@example.com" {
		t.Errorf("expected B's stale email not to be written, got %v", organisation.Email)
	}

	// B reloads (new ETag) and can then save deliberately.
	if retry := doRequestWithIfMatch(handler, path, sessionB, `{"email":"new@example.com"}`, etagOf(t, handler, path, sessionB)); retry.Code != http.StatusOK {
		t.Errorf("expected B's save after reloading to succeed, got %d (body: %s)", retry.Code, retry.Body.String())
	}
}

func TestApp_OptimisticConcurrency_SettingsPreconditions(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "OCC Settings Org", "occ-settings-"+uuid.NewString()+"@example.com")
	const path = "/api/v1/organisation/settings"
	body := `{"paymentTerms":14}`

	if recorder := doRequestWithIfMatch(handler, path, tenant.token, body, ""); recorder.Code != http.StatusPreconditionRequired {
		t.Errorf("missing If-Match: expected 428, got %d", recorder.Code)
	}
	if recorder := doRequestWithIfMatch(handler, path, tenant.token, body, `W/"1"`); recorder.Code != http.StatusBadRequest {
		t.Errorf("weak If-Match: expected 400, got %d", recorder.Code)
	}

	etag := etagOf(t, handler, path, tenant.token)
	saved := doRequestWithIfMatch(handler, path, tenant.token, body, etag)
	if saved.Code != http.StatusOK || saved.Header().Get("ETag") == etag {
		t.Fatalf("expected 200 with a new ETag, got %d ETag=%q", saved.Code, saved.Header().Get("ETag"))
	}
	if stale := doRequestWithIfMatch(handler, path, tenant.token, `{"paymentTerms":7}`, etag); stale.Code != http.StatusPreconditionFailed {
		t.Errorf("stale If-Match: expected 412, got %d", stale.Code)
	}
}

// Invoice creation allocates an invoice number (an internal settings
// write) — it must not change the settings ETag, or every invoice created
// by anyone would make an admin's open settings form stale.
func TestApp_OptimisticConcurrency_InvoiceCreationDoesNotChangeSettingsETag(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "OCC Numbering Org", "occ-numbering-"+uuid.NewString()+"@example.com")
	const path = "/api/v1/organisation/settings"

	before := etagOf(t, handler, path, tenant.token)

	customer := doRequest(handler, http.MethodPost, "/api/v1/customers", tenant.token, bytes.NewBufferString(`{"name":"Numbering Customer"}`))
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(customer.Body).Decode(&created)
	for i := 0; i < 2; i++ {
		invoice := doRequest(handler, http.MethodPost, "/api/v1/invoices", tenant.token, bytes.NewBufferString(`{
			"customerId": "`+created.ID+`", "issueDate": "2026-01-01",
			"dueDate": "`+time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")+`",
			"lines": [{"description": "Work", "quantity": 1, "unitPrice": 100, "vatRate": 0}]
		}`))
		if invoice.Code != http.StatusCreated {
			t.Fatalf("create invoice: status %d (body: %s)", invoice.Code, invoice.Body.String())
		}
	}

	if after := etagOf(t, handler, path, tenant.token); after != before {
		t.Errorf("expected invoice creation to leave the settings ETag at %s, got %s", before, after)
	}
	if recorder := doRequestWithIfMatch(handler, path, tenant.token, `{"paymentTerms":21}`, before); recorder.Code != http.StatusOK {
		t.Errorf("expected a save with the pre-invoice ETag to succeed, got %d", recorder.Code)
	}
}

// The role gate still runs first: a non-admin gets 403 whether or not it
// sends If-Match, never a 428/412 that would reveal anything about the
// precondition. Each tenant's version is its own.
func TestApp_OptimisticConcurrency_RoleAndTenantRestrictionsUnchanged(t *testing.T) {
	handler, db := newTestApp(t)
	admin := registerTenant(t, handler, db, "OCC Roles Org", "occ-roles-admin-"+uuid.NewString()+"@example.com")
	_, userToken := createAndLoginUser(t, handler, admin.token, "Plain User", "occ-roles-user-"+uuid.NewString()+"@example.com", "user")
	other := registerTenant(t, handler, db, "OCC Other Org", "occ-other-"+uuid.NewString()+"@example.com")

	for _, path := range []string{"/api/v1/organisation", "/api/v1/organisation/settings"} {
		etag := etagOf(t, handler, path, userToken) // every role may still GET
		for _, ifMatch := range []string{"", etag, `"999"`} {
			if recorder := doRequestWithIfMatch(handler, path, userToken, `{}`, ifMatch); recorder.Code != http.StatusForbidden {
				t.Errorf("%s as user, If-Match %q: expected 403, got %d", path, ifMatch, recorder.Code)
			}
		}

		// Another tenant's update never affects this tenant's version.
		if recorder := doRequestWithIfMatch(handler, path, other.token, `{}`, etagOf(t, handler, path, other.token)); recorder.Code != http.StatusOK {
			t.Fatalf("%s: other tenant's update failed: %d", path, recorder.Code)
		}
		if after := etagOf(t, handler, path, admin.token); after != etag {
			t.Errorf("%s: expected this tenant's ETag to stay %s after another tenant's update, got %s", path, etag, after)
		}
	}
}
