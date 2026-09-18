package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	admin "go-invoicing/internal/administration"
)

// This is intentionally the first mux/application-level integration test
// in this project: every other package tests its handlers directly,
// bypassing app.Handler() entirely. Authentication middleware and
// role-based authorisation are both wired onto specific routes inside
// app.go itself, so whether that wiring is correct has no other test
// surface; a per-handler unit test can't see it.

// newTestPool mirrors the same DATABASE_URL-gated helper used throughout
// internal/administration's and internal/invoice's Postgres integration
// tests: skip (not fail) when no database is configured, and close via
// t.Cleanup so it runs after any row-delete cleanups registered later.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func newTestApp(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()

	db := newTestPool(t)
	return New(db).Handler(), db
}

func TestApp_PublicHealthRoutes_WorkWithoutAuthorization(t *testing.T) {
	handler, _ := newTestApp(t)

	for _, path := range []string{"/health", "/health/db"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestApp_PostAuthLogin_IsNotInterceptedByMiddleware sends no Authorization
// header at all. If this route were mistakenly wrapped by
// AuthMiddleware, the response would be the middleware's generic 401
// "unauthorized" with a WWW-Authenticate header; instead it must reach
// AuthHandler.Login itself, whose own validation rejects the empty body
// with 400 and no such header.
func TestApp_PostAuthLogin_IsNotInterceptedByMiddleware(t *testing.T) {
	handler, _ := newTestApp(t)

	body := bytes.NewBufferString(`{}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}

	if recorder.Header().Get("WWW-Authenticate") != "" {
		t.Error("expected no WWW-Authenticate header — this route must not be handled by AuthMiddleware")
	}
}

// TestApp_PostRegister_IsNotInterceptedByMiddleware mirrors the login
// test above for the new public registration route.
func TestApp_PostRegister_IsNotInterceptedByMiddleware(t *testing.T) {
	handler, _ := newTestApp(t)

	body := bytes.NewBufferString(`{}`)
	request := httptest.NewRequest(http.MethodPost, "/register", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}

	if recorder.Header().Get("WWW-Authenticate") != "" {
		t.Error("expected no WWW-Authenticate header — this route must not be handled by AuthMiddleware")
	}
}

// TestApp_OrganisationsRouteRemoved proves the old public bootstrap route
// is gone entirely — not merely protected, not left as an alternate path
// — Milestone 4 Part 5 replaces it with POST /register. An unregistered
// net/http ServeMux pattern falls through to the mux's own 404 handler.
func TestApp_OrganisationsRouteRemoved(t *testing.T) {
	handler, _ := newTestApp(t)

	body := bytes.NewBufferString(`{"name":"Should Not Work"}`)
	request := httptest.NewRequest(http.MethodPost, "/organisations", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (route should no longer exist), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// TestApp_PostUsers_RequiresAuthentication proves POST /users is no
// longer a public bootstrap route: with no Authorization header at all,
// it must now reject with 401, exactly like every other protected route.
func TestApp_PostUsers_RequiresAuthentication(t *testing.T) {
	handler, _ := newTestApp(t)

	body := bytes.NewBufferString(`{"name":"Should Not Work","email":"nobody@example.com","password":"correct horse battery staple"}`)
	request := httptest.NewRequest(http.MethodPost, "/users", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestApp_ProtectedRoutes_RejectRequestsWithoutAuthorization sweeps every
// protected route, without duplicating each domain package's own handler
// test suite — this only checks that the middleware is actually wired
// onto each one in app.go, which is the one thing those per-handler tests
// can't see (they call the handler function directly, never through
// app.Handler()).
func TestApp_ProtectedRoutes_RejectRequestsWithoutAuthorization(t *testing.T) {
	handler, _ := newTestApp(t)

	id := uuid.New().String()

	protectedRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/organisation"},
		{http.MethodPatch, "/organisation"},
		{http.MethodPost, "/users"},
		{http.MethodGet, "/users/" + id},
		{http.MethodPost, "/customers"},
		{http.MethodGet, "/customers/" + id},
		{http.MethodGet, "/customers/" + id + "/billing-address"},
		{http.MethodPut, "/customers/" + id + "/billing-address"},
		{http.MethodPost, "/products"},
		{http.MethodGet, "/products/" + id},
		{http.MethodPost, "/invoices"},
		{http.MethodGet, "/invoices/" + id},
		{http.MethodPost, "/invoices/" + id + "/send"},
		{http.MethodPost, "/invoices/" + id + "/payments"},
		{http.MethodGet, "/invoices/" + id + "/payments"},
	}

	for _, route := range protectedRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, bytes.NewBufferString(`{}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
			}

			if got := recorder.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("expected WWW-Authenticate: Bearer, got %q", got)
			}
		})
	}
}

// --- Shared registration/login test helpers ---

const testPassword = "correct horse battery staple"

type testTenant struct {
	organisationID string
	userID         string
	token          string
}

// registerTenant drives the real public registration + login flow —
// POST /register then POST /auth/login — the only way an organisation
// and its first (admin) user may be created after Milestone 4 Part 5.
func registerTenant(t *testing.T, handler http.Handler, db *pgxpool.Pool, orgName, adminEmail string) testTenant {
	t.Helper()

	registerBody := bytes.NewBufferString(`{
		"organisation": {"name": "` + orgName + `"},
		"user": {"name": "` + orgName + ` Admin", "email": "` + adminEmail + `", "password": "` + testPassword + `"}
	}`)
	registerRecorder := doRequest(handler, http.MethodPost, "/register", "", registerBody)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register %q: status %d (body: %s)", orgName, registerRecorder.Code, registerRecorder.Body.String())
	}

	var registerResponse struct {
		Organisation struct {
			ID string `json:"id"`
		} `json:"organisation"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(registerRecorder.Body).Decode(&registerResponse); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	t.Cleanup(func() { cleanupOrganisation(db, registerResponse.Organisation.ID) })

	token := loginAs(t, handler, adminEmail, testPassword)

	return testTenant{
		organisationID: registerResponse.Organisation.ID,
		userID:         registerResponse.User.ID,
		token:          token,
	}
}

// loginAs calls POST /auth/login and returns the bearer token.
func loginAs(t *testing.T, handler http.Handler, email, password string) string {
	t.Helper()

	loginBody := bytes.NewBufferString(`{"email":"` + email + `","password":"` + password + `"}`)
	loginRecorder := doRequest(handler, http.MethodPost, "/auth/login", "", loginBody)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login as %q: status %d (body: %s)", email, loginRecorder.Code, loginRecorder.Body.String())
	}

	var loginResponse struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(loginRecorder.Body).Decode(&loginResponse); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	return loginResponse.Token
}

// createUser drives POST /users as actorToken, returning the raw
// response recorder so callers can assert on status themselves (some
// tests expect this to fail, e.g. privilege escalation attempts).
func createUser(handler http.Handler, actorToken, name, email, role string) *httptest.ResponseRecorder {
	body := bytes.NewBufferString(`{"name":"` + name + `","email":"` + email + `","password":"` + testPassword + `","role":"` + role + `"}`)
	return doRequest(handler, http.MethodPost, "/users", actorToken, body)
}

// createAndLoginUser creates a user as actorToken (expected to succeed)
// and immediately logs them in, returning their own token and ID.
func createAndLoginUser(t *testing.T, handler http.Handler, actorToken, name, email, role string) (userID, token string) {
	t.Helper()

	recorder := createUser(handler, actorToken, name, email, role)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create user %q (role %s): status %d (body: %s)", email, role, recorder.Code, recorder.Body.String())
	}

	var response struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode create-user response: %v", err)
	}

	return response.ID, loginAs(t, handler, email, testPassword)
}

// cleanupOrganisation removes every row this test package could have
// created for organisationID, in FK-safe order. Best-effort: errors are
// ignored, matching every other Postgres integration test's cleanup
// convention in this repo.
func cleanupOrganisation(db *pgxpool.Pool, organisationID string) {
	ctx := context.Background()
	_, _ = db.Exec(ctx, "DELETE FROM payments WHERE invoice_id IN (SELECT id FROM invoices WHERE organisation_id = $1)", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM invoice_lines WHERE invoice_id IN (SELECT id FROM invoices WHERE organisation_id = $1)", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM invoices WHERE organisation_id = $1", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM products WHERE organisation_id = $1", organisationID)
	// addresses has no organisation_id column of its own (Milestone 7 Part
	// 1) — scoped via customers, and must be deleted before customers
	// itself, or the customers delete below silently fails on the
	// addresses_customer_id_fkey foreign key.
	_, _ = db.Exec(ctx, "DELETE FROM addresses WHERE customer_id IN (SELECT id FROM customers WHERE organisation_id = $1)", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM customers WHERE organisation_id = $1", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE organisation_id = $1)", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM users WHERE organisation_id = $1", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM settings WHERE organisation_id = $1", organisationID)
	_, _ = db.Exec(ctx, "DELETE FROM organisations WHERE id = $1", organisationID)
}

func doRequest(handler http.Handler, method, path, token string, body *bytes.Buffer) *httptest.ResponseRecorder {
	if body == nil {
		body = &bytes.Buffer{}
	}
	request := httptest.NewRequest(method, path, body)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// --- End-to-end registration -> login -> authenticated operation ---

// TestApp_EndToEnd_RegisterLoginThenAccessProtectedRoute exercises the
// full, real-Postgres-backed pipeline this milestone assembles:
// POST /register (public) -> POST /auth/login (public) -> use the
// returned bearer token for GET /organisation and a normal authenticated
// business-data operation.
func TestApp_EndToEnd_RegisterLoginThenAccessProtectedRoute(t *testing.T) {
	handler, db := newTestApp(t)

	tenant := registerTenant(t, handler, db, "E2E Test Org", "e2e-admin@example.com")

	orgRecorder := doRequest(handler, http.MethodGet, "/organisation", tenant.token, nil)
	if orgRecorder.Code != http.StatusOK {
		t.Fatalf("GET /organisation: status %d (body: %s)", orgRecorder.Code, orgRecorder.Body.String())
	}

	var orgResponse struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(orgRecorder.Body).Decode(&orgResponse); err != nil {
		t.Fatalf("decode organisation response: %v", err)
	}
	if orgResponse.ID != tenant.organisationID {
		t.Errorf("expected GET /organisation to return %q, got %q", tenant.organisationID, orgResponse.ID)
	}

	customerRecorder := doRequest(handler, http.MethodPost, "/customers", tenant.token, bytes.NewBufferString(`{"name":"E2E Customer"}`))
	if customerRecorder.Code != http.StatusCreated {
		t.Fatalf("create customer: status %d (body: %s)", customerRecorder.Code, customerRecorder.Body.String())
	}

	// Without the token, the same routes must reject the request.
	if recorder := doRequest(handler, http.MethodGet, "/organisation", "", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d without a token, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

// --- Role-based authorisation matrix ---

// TestApp_RoleMatrix_UserManagement is a single table-driven test
// covering every user-management privilege rule end-to-end, rather than
// one hand-written test function per role/target-role permutation.
func TestApp_RoleMatrix_UserManagement(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Role Matrix Org", "role-matrix-admin@example.com")

	_, managerToken := createAndLoginUser(t, handler, tenant.token, "Manager", "role-matrix-manager@example.com", admin.UserRoleManager)
	_, userToken := createAndLoginUser(t, handler, tenant.token, "User", "role-matrix-user@example.com", admin.UserRoleUser)

	tests := []struct {
		name       string
		actorToken string
		email      string
		targetRole string
		wantStatus int
	}{
		{"admin creates admin", tenant.token, "matrix-admin-1@example.com", admin.UserRoleAdmin, http.StatusCreated},
		{"admin creates manager", tenant.token, "matrix-admin-2@example.com", admin.UserRoleManager, http.StatusCreated},
		{"admin creates user", tenant.token, "matrix-admin-3@example.com", admin.UserRoleUser, http.StatusCreated},
		{"manager creates user", managerToken, "matrix-manager-1@example.com", admin.UserRoleUser, http.StatusCreated},
		{"manager cannot create manager", managerToken, "matrix-manager-2@example.com", admin.UserRoleManager, http.StatusForbidden},
		{"manager cannot create admin", managerToken, "matrix-manager-3@example.com", admin.UserRoleAdmin, http.StatusForbidden},
		{"user cannot create user", userToken, "matrix-user-1@example.com", admin.UserRoleUser, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := createUser(handler, tt.actorToken, "Matrix Target", tt.email, tt.targetRole)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestApp_RoleMatrix_OrganisationUpdate proves PATCH /organisation
// (Milestone 7 Part 1) is admin-only: manager and user must both be
// rejected with 403, exactly like every other admin-only route in this
// project, while GET /organisation remains open to all three roles
// (already covered by TestApp_EndToEnd_RegisterLoginThenAccessProtectedRoute
// and the cross-tenant test).
func TestApp_RoleMatrix_OrganisationUpdate(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Org Update Role Org", "org-update-admin@example.com")

	_, managerToken := createAndLoginUser(t, handler, tenant.token, "Manager", "org-update-manager@example.com", admin.UserRoleManager)
	_, userToken := createAndLoginUser(t, handler, tenant.token, "User", "org-update-user@example.com", admin.UserRoleUser)

	tests := []struct {
		name       string
		actorToken string
		wantStatus int
	}{
		{"admin can update organisation", tenant.token, http.StatusOK},
		{"manager cannot update organisation", managerToken, http.StatusForbidden},
		{"user cannot update organisation", userToken, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := doRequest(handler, http.MethodPatch, "/organisation", tt.actorToken, bytes.NewBufferString(`{"city":"London"}`))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestApp_OrganisationUpdate_EndToEnd exercises genuine partial-update
// semantics through the real HTTP/service/repository/PostgreSQL stack:
// an update that only supplies Email must leave a previously-set Phone
// untouched, and the change must be visible on a subsequent GET.
func TestApp_OrganisationUpdate_EndToEnd(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Org Update E2E Org", "org-update-e2e-admin@example.com")

	firstRecorder := doRequest(handler, http.MethodPatch, "/organisation", tenant.token, bytes.NewBufferString(`{"phone":"+44 20 7946 0958"}`))
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first update: status %d (body: %s)", firstRecorder.Code, firstRecorder.Body.String())
	}

	secondRecorder := doRequest(handler, http.MethodPatch, "/organisation", tenant.token, bytes.NewBufferString(`{"email":"contact@org-update-e2e.test"}`))
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second update: status %d (body: %s)", secondRecorder.Code, secondRecorder.Body.String())
	}

	getRecorder := doRequest(handler, http.MethodGet, "/organisation", tenant.token, nil)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get organisation: status %d (body: %s)", getRecorder.Code, getRecorder.Body.String())
	}

	var response struct {
		Phone *string `json:"phone"`
		Email *string `json:"email"`
	}
	if err := json.NewDecoder(getRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode organisation response: %v", err)
	}

	if response.Phone == nil || *response.Phone != "+44 20 7946 0958" {
		t.Errorf("expected phone to remain set from the first update, got %v", response.Phone)
	}

	if response.Email == nil || *response.Email != "contact@org-update-e2e.test" {
		t.Errorf("expected email to be set by the second update, got %v", response.Email)
	}
}

// TestApp_ManagerPrivilegeEscalation_EndToEnd is the explicit end-to-end
// proof that a manager cannot escalate their own organisation's
// privilege structure by creating an admin — the crux security guarantee
// of the user-management rules, isolated as its own test rather than
// buried in the table above.
func TestApp_ManagerPrivilegeEscalation_EndToEnd(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Escalation Test Org", "escalation-admin@example.com")

	_, managerToken := createAndLoginUser(t, handler, tenant.token, "Manager", "escalation-manager@example.com", admin.UserRoleManager)

	recorder := createUser(handler, managerToken, "Sneaky Admin", "escalation-sneaky-admin@example.com", admin.UserRoleAdmin)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	if recorder.Body.String() != "forbidden\n" {
		t.Errorf("expected generic body %q, got %q", "forbidden\n", recorder.Body.String())
	}

	// The rejected email must not have been consumed — a genuine admin
	// registration with the same email must still succeed elsewhere,
	// proving nothing was partially created.
	loginRecorder := doRequest(handler, http.MethodPost, "/auth/login", "", bytes.NewBufferString(`{"email":"escalation-sneaky-admin@example.com","password":"`+testPassword+`"}`))
	if loginRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected login for the never-created user to fail with status %d, got %d", http.StatusUnauthorized, loginRecorder.Code)
	}
}

// TestApp_RoleMatrix_BusinessDataOperationsAvailableToAllRoles proves
// every authenticated role — admin, manager, user — retains full access
// to the actual business-data operations (customers, products, invoices,
// payments): Milestone 4 Part 5 only restricts user-management, not
// day-to-day invoicing work.
func TestApp_RoleMatrix_BusinessDataOperationsAvailableToAllRoles(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Business Data Org", "business-data-admin@example.com")

	_, managerToken := createAndLoginUser(t, handler, tenant.token, "Manager", "business-data-manager@example.com", admin.UserRoleManager)
	_, userToken := createAndLoginUser(t, handler, tenant.token, "User", "business-data-user@example.com", admin.UserRoleUser)

	roles := []struct {
		name  string
		token string
	}{
		{"admin", tenant.token},
		{"manager", managerToken},
		{"user", userToken},
	}

	for _, role := range roles {
		t.Run(role.name, func(t *testing.T) {
			customerRecorder := doRequest(handler, http.MethodPost, "/customers", role.token, bytes.NewBufferString(`{"name":"`+role.name+`'s Customer"}`))
			if customerRecorder.Code != http.StatusCreated {
				t.Fatalf("create customer as %s: status %d (body: %s)", role.name, customerRecorder.Code, customerRecorder.Body.String())
			}
			var customer struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(customerRecorder.Body).Decode(&customer)

			if recorder := doRequest(handler, http.MethodGet, "/customers/"+customer.ID, role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get customer as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			// Billing address (Milestone 7 Part 1): all three roles may
			// create/read a customer's billing address, same policy as
			// every other customer/invoice business-data route.
			billingAddressBody := bytes.NewBufferString(`{"street":"1 ` + role.name + ` Way","city":"London","postalCode":"E1 6AN","country":"GB"}`)
			billingAddressRecorder := doRequest(handler, http.MethodPut, "/customers/"+customer.ID+"/billing-address", role.token, billingAddressBody)
			if billingAddressRecorder.Code != http.StatusOK {
				t.Fatalf("put billing address as %s: status %d (body: %s)", role.name, billingAddressRecorder.Code, billingAddressRecorder.Body.String())
			}

			if recorder := doRequest(handler, http.MethodGet, "/customers/"+customer.ID+"/billing-address", role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get billing address as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			productRecorder := doRequest(handler, http.MethodPost, "/products", role.token, bytes.NewBufferString(`{"name":"`+role.name+`'s Product","sku":"SKU-`+role.name+`","price":500}`))
			if productRecorder.Code != http.StatusCreated {
				t.Fatalf("create product as %s: status %d (body: %s)", role.name, productRecorder.Code, productRecorder.Body.String())
			}
			var product struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(productRecorder.Body).Decode(&product)

			if recorder := doRequest(handler, http.MethodGet, "/products/"+product.ID, role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get product as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			invoiceBody := bytes.NewBufferString(`{
				"customerId": "` + customer.ID + `",
				"issueDate": "2026-01-01",
				"dueDate": "2026-01-31",
				"lines": [{"description": "Service", "quantity": 1, "unitPrice": 1000, "vatRate": 0}]
			}`)
			invoiceRecorder := doRequest(handler, http.MethodPost, "/invoices", role.token, invoiceBody)
			if invoiceRecorder.Code != http.StatusCreated {
				t.Fatalf("create invoice as %s: status %d (body: %s)", role.name, invoiceRecorder.Code, invoiceRecorder.Body.String())
			}
			var invoice struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(invoiceRecorder.Body).Decode(&invoice)

			if recorder := doRequest(handler, http.MethodGet, "/invoices/"+invoice.ID, role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get invoice as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			// A newly-created invoice is Draft and cannot accept a payment
			// (Milestone 5) — send it first. This also proves all three
			// roles may use the new Send endpoint.
			sendRecorder := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/send", role.token, nil)
			if sendRecorder.Code != http.StatusOK {
				t.Fatalf("send invoice as %s: status %d (body: %s)", role.name, sendRecorder.Code, sendRecorder.Body.String())
			}

			paymentRecorder := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/payments", role.token, bytes.NewBufferString(`{"amount":100,"paymentMethod":"cash","paymentDate":"2026-01-15"}`))
			if paymentRecorder.Code != http.StatusCreated {
				t.Fatalf("create payment as %s: status %d (body: %s)", role.name, paymentRecorder.Code, paymentRecorder.Body.String())
			}

			if recorder := doRequest(handler, http.MethodGet, "/invoices/"+invoice.ID+"/payments", role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get payments as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestApp_UserRetrieval_SelfVsOther proves the self-vs-other GET
// /users/{id} rule end-to-end: every role may retrieve themselves;
// retrieving another user in the same organisation requires admin or
// manager.
func TestApp_UserRetrieval_SelfVsOther(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Self Vs Other Org", "self-other-admin@example.com")

	managerID, managerToken := createAndLoginUser(t, handler, tenant.token, "Manager", "self-other-manager@example.com", admin.UserRoleManager)
	userID, userToken := createAndLoginUser(t, handler, tenant.token, "User", "self-other-user@example.com", admin.UserRoleUser)

	tests := []struct {
		name       string
		actorToken string
		targetID   string
		wantStatus int
	}{
		{"user can GET self", userToken, userID, http.StatusOK},
		{"user gets 403 for another user", userToken, managerID, http.StatusForbidden},
		{"manager can GET another user", managerToken, userID, http.StatusOK},
		{"admin can GET another user", tenant.token, userID, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := doRequest(handler, http.MethodGet, "/users/"+tt.targetID, tt.actorToken, nil)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// --- Two-organisation cross-tenant isolation ---

// --- Mid-session deactivation / soft-deletion (Milestone 4 Part 6) ---
//
// These prove the core architectural guarantee behind choosing
// server-side sessions over JWT (see Milestone 4 Part 2's design
// investigation): a user's current state is re-checked on every request,
// so deactivating or deleting them takes effect immediately — even for a
// session token issued before that happened — with no logout or
// revocation step required. Neither test adds a deactivation/deletion
// endpoint; both mutate the row directly via the test's own database
// handle, exactly as the task describes.

// TestApp_MidSessionDeactivation_TokenStopsWorking proves a bearer token
// that worked a moment ago stops working the instant the underlying user
// is deactivated, with no other change to the token or session.
func TestApp_MidSessionDeactivation_TokenStopsWorking(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Deactivation Org", "deactivation-admin@example.com")

	userID, token := createAndLoginUser(t, handler, tenant.token, "Target", "deactivation-target@example.com", admin.UserRoleUser)

	if recorder := doRequest(handler, http.MethodGet, "/users/"+userID, token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d before deactivation, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if _, err := db.Exec(context.Background(), "UPDATE users SET is_active = false WHERE id = $1", userID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	recorder := doRequest(handler, http.MethodGet, "/users/"+userID, token, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d after deactivation with the same token, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestApp_MidSessionSoftDeletion_TokenStopsWorking mirrors the
// deactivation test for soft-deletion: GetByIDForAuthentication's
// deleted_at IS NULL filter must reject the session just as reliably as
// the is_active check does.
func TestApp_MidSessionSoftDeletion_TokenStopsWorking(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Soft Delete Org", "softdelete-admin@example.com")

	userID, token := createAndLoginUser(t, handler, tenant.token, "Target", "softdelete-target@example.com", admin.UserRoleUser)

	if recorder := doRequest(handler, http.MethodGet, "/users/"+userID, token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d before soft-deletion, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if _, err := db.Exec(context.Background(), "UPDATE users SET deleted_at = NOW() WHERE id = $1", userID); err != nil {
		t.Fatalf("soft-delete user: %v", err)
	}

	recorder := doRequest(handler, http.MethodGet, "/users/"+userID, token, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d after soft-deletion with the same token, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestApp_InvoiceLifecycle_DraftSentPaid is the Milestone 5 end-to-end
// happy path: Create (Draft) -> payment rejected -> Send (Sent) ->
// repeated Send rejected -> partial payment (still Sent) -> final
// payment (Paid) -> Send after Paid rejected. The due date is computed a
// full year out specifically so this test never becomes flaky as real
// time passes — the exact day-before/on/day-after Overdue boundary is
// deliberately tested at the domain layer (invoice_test.go) instead of
// here, per Milestone 5's own guidance that current-time-dependent HTTP
// assertions would be brittle.
func TestApp_InvoiceLifecycle_DraftSentPaid(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Lifecycle Org", "lifecycle-admin@example.com")

	customerRecorder := doRequest(handler, http.MethodPost, "/customers", tenant.token, bytes.NewBufferString(`{"name":"Lifecycle Customer"}`))
	if customerRecorder.Code != http.StatusCreated {
		t.Fatalf("create customer: status %d (body: %s)", customerRecorder.Code, customerRecorder.Body.String())
	}
	var customer struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(customerRecorder.Body).Decode(&customer)

	farFutureDueDate := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	invoiceBody := bytes.NewBufferString(`{
		"customerId": "` + customer.ID + `",
		"issueDate": "2026-01-01",
		"dueDate": "` + farFutureDueDate + `",
		"lines": [{"description": "Consulting", "quantity": 1, "unitPrice": 1000, "vatRate": 0}]
	}`)
	invoiceRecorder := doRequest(handler, http.MethodPost, "/invoices", tenant.token, invoiceBody)
	if invoiceRecorder.Code != http.StatusCreated {
		t.Fatalf("create invoice: status %d (body: %s)", invoiceRecorder.Code, invoiceRecorder.Body.String())
	}
	var invoice struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(invoiceRecorder.Body).Decode(&invoice); err != nil {
		t.Fatalf("decode invoice response: %v", err)
	}
	if invoice.Status != "draft" {
		t.Fatalf("expected a newly-created invoice to be %q, got %q", "draft", invoice.Status)
	}

	// Draft cannot accept payment.
	paymentAgainstDraft := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/payments", tenant.token, bytes.NewBufferString(`{"amount":1000,"paymentMethod":"cash","paymentDate":"2026-01-15"}`))
	if paymentAgainstDraft.Code != http.StatusConflict {
		t.Fatalf("expected status %d paying a Draft invoice, got %d (body: %s)", http.StatusConflict, paymentAgainstDraft.Code, paymentAgainstDraft.Body.String())
	}

	// Draft -> Sent.
	sendRecorder := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/send", tenant.token, nil)
	if sendRecorder.Code != http.StatusOK {
		t.Fatalf("send invoice: status %d (body: %s)", sendRecorder.Code, sendRecorder.Body.String())
	}
	var sentInvoice struct {
		Status string  `json:"status"`
		SentAt *string `json:"sentAt"`
	}
	if err := json.NewDecoder(sendRecorder.Body).Decode(&sentInvoice); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if sentInvoice.Status != "sent" {
		t.Errorf("expected status %q after send, got %q", "sent", sentInvoice.Status)
	}
	if sentInvoice.SentAt == nil || *sentInvoice.SentAt == "" {
		t.Error("expected sentAt to be populated after send")
	}

	// Repeated send is rejected, not silently successful.
	repeatSendRecorder := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/send", tenant.token, nil)
	if repeatSendRecorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d for a repeated send, got %d (body: %s)", http.StatusConflict, repeatSendRecorder.Code, repeatSendRecorder.Body.String())
	}

	// Partial payment: still Sent (due date is a year out, never overdue
	// for the life of this test).
	partialPaymentRecorder := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/payments", tenant.token, bytes.NewBufferString(`{"amount":400,"paymentMethod":"cash","paymentDate":"2026-01-15"}`))
	if partialPaymentRecorder.Code != http.StatusCreated {
		t.Fatalf("create partial payment: status %d (body: %s)", partialPaymentRecorder.Code, partialPaymentRecorder.Body.String())
	}

	afterPartialRecorder := doRequest(handler, http.MethodGet, "/invoices/"+invoice.ID, tenant.token, nil)
	var afterPartial struct {
		Status     string `json:"status"`
		AmountPaid int64  `json:"amountPaid"`
	}
	_ = json.NewDecoder(afterPartialRecorder.Body).Decode(&afterPartial)
	if afterPartial.Status != "sent" {
		t.Errorf("expected status to remain %q after a partial payment, got %q", "sent", afterPartial.Status)
	}
	if afterPartial.AmountPaid != 400 {
		t.Errorf("expected amountPaid 400, got %d", afterPartial.AmountPaid)
	}

	// Final payment: Sent -> Paid.
	finalPaymentRecorder := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/payments", tenant.token, bytes.NewBufferString(`{"amount":600,"paymentMethod":"cash","paymentDate":"2026-01-20"}`))
	if finalPaymentRecorder.Code != http.StatusCreated {
		t.Fatalf("create final payment: status %d (body: %s)", finalPaymentRecorder.Code, finalPaymentRecorder.Body.String())
	}

	afterFinalRecorder := doRequest(handler, http.MethodGet, "/invoices/"+invoice.ID, tenant.token, nil)
	var afterFinal struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(afterFinalRecorder.Body).Decode(&afterFinal)
	if afterFinal.Status != "paid" {
		t.Errorf("expected status %q after the final payment, got %q", "paid", afterFinal.Status)
	}

	// Paid invoices can never be (re-)sent.
	sendAfterPaidRecorder := doRequest(handler, http.MethodPost, "/invoices/"+invoice.ID+"/send", tenant.token, nil)
	if sendAfterPaidRecorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d sending a Paid invoice, got %d (body: %s)", http.StatusConflict, sendAfterPaidRecorder.Code, sendAfterPaidRecorder.Body.String())
	}
}

// TestApp_CrossTenantIsolation_TwoOrganisations is the genuine
// two-organisation end-to-end isolation test: two real organisations,
// each with its own real logged-in admin, proving through the full
// mux -> middleware -> handler -> service -> repository -> Postgres
// chain that Organisation A's authenticated user can never read or
// mutate Organisation B's customer, product, invoice, payments,
// user, or organisation — including when A deliberately supplies
// ?organisationId=<Organisation B> on the request, which must have zero
// effect on the outcome.
func TestApp_CrossTenantIsolation_TwoOrganisations(t *testing.T) {
	handler, db := newTestApp(t)

	tenantA := registerTenant(t, handler, db, "Tenant A", "tenant-a-user@example.com")
	tenantB := registerTenant(t, handler, db, "Tenant B", "tenant-b-user@example.com")

	// GET /organisation must always resolve to the caller's own
	// organisation — there is no ID in the URL for a client to manipulate.
	orgARecorder := doRequest(handler, http.MethodGet, "/organisation", tenantA.token, nil)
	if orgARecorder.Code != http.StatusOK {
		t.Fatalf("GET /organisation as tenant A: status %d (body: %s)", orgARecorder.Code, orgARecorder.Body.String())
	}
	var orgAResponse struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(orgARecorder.Body).Decode(&orgAResponse); err != nil {
		t.Fatalf("decode organisation response: %v", err)
	}
	if orgAResponse.ID != tenantA.organisationID {
		t.Errorf("expected GET /organisation as tenant A to return %q, got %q", tenantA.organisationID, orgAResponse.ID)
	}

	// Tenant B creates a customer, a product, and an invoice against that
	// customer — all as itself, all protected routes.
	customerBRecorder := doRequest(handler, http.MethodPost, "/customers", tenantB.token, bytes.NewBufferString(`{"name":"Tenant B Customer"}`))
	if customerBRecorder.Code != http.StatusCreated {
		t.Fatalf("create tenant B customer: status %d (body: %s)", customerBRecorder.Code, customerBRecorder.Body.String())
	}
	var customerB struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(customerBRecorder.Body).Decode(&customerB); err != nil {
		t.Fatalf("decode customer response: %v", err)
	}

	productBRecorder := doRequest(handler, http.MethodPost, "/products", tenantB.token, bytes.NewBufferString(`{"name":"Tenant B Product","sku":"TB-SKU-1","price":500}`))
	if productBRecorder.Code != http.StatusCreated {
		t.Fatalf("create tenant B product: status %d (body: %s)", productBRecorder.Code, productBRecorder.Body.String())
	}
	var productB struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(productBRecorder.Body).Decode(&productB); err != nil {
		t.Fatalf("decode product response: %v", err)
	}

	invoiceBBody := bytes.NewBufferString(`{
		"customerId": "` + customerB.ID + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": [{"description": "Service", "quantity": 1, "unitPrice": 1000, "vatRate": 0}]
	}`)
	invoiceBRecorder := doRequest(handler, http.MethodPost, "/invoices", tenantB.token, invoiceBBody)
	if invoiceBRecorder.Code != http.StatusCreated {
		t.Fatalf("create tenant B invoice: status %d (body: %s)", invoiceBRecorder.Code, invoiceBRecorder.Body.String())
	}
	var invoiceB struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(invoiceBRecorder.Body).Decode(&invoiceB); err != nil {
		t.Fatalf("decode invoice response: %v", err)
	}

	// Every one of the following must be blocked for tenant A — both
	// with no query parameter at all, and with a deliberately supplied
	// ?organisationId=<tenant B> that must have zero effect.
	crossTenantCases := []struct {
		name   string
		method string
		path   string
		body   string // built fresh into a new *bytes.Buffer per request — a *bytes.Buffer is drained after one use
	}{
		{"get tenant B customer", http.MethodGet, "/customers/" + customerB.ID, ""},
		{"get tenant B customer's billing address", http.MethodGet, "/customers/" + customerB.ID + "/billing-address", ""},
		{"put tenant B customer's billing address", http.MethodPut, "/customers/" + customerB.ID + "/billing-address", `{"street":"Attacker St","city":"X","postalCode":"00000","country":"XX"}`},
		{"get tenant B product", http.MethodGet, "/products/" + productB.ID, ""},
		{"get tenant B invoice", http.MethodGet, "/invoices/" + invoiceB.ID, ""},
		{"send tenant B invoice", http.MethodPost, "/invoices/" + invoiceB.ID + "/send", ""},
		{"create payment against tenant B invoice", http.MethodPost, "/invoices/" + invoiceB.ID + "/payments", `{"amount":100,"paymentMethod":"cash","paymentDate":"2026-01-15"}`},
		{"list payments for tenant B invoice", http.MethodGet, "/invoices/" + invoiceB.ID + "/payments", ""},
		{"get tenant B user", http.MethodGet, "/users/" + tenantB.userID, ""},
	}

	for _, c := range crossTenantCases {
		t.Run(c.name+" (no query parameter)", func(t *testing.T) {
			recorder := doRequest(handler, c.method, c.path, tenantA.token, bytes.NewBufferString(c.body))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
			}
		})

		t.Run(c.name+" (organisationId query parameter naming tenant B)", func(t *testing.T) {
			pathWithMaliciousQuery := c.path + "?organisationId=" + tenantB.organisationID
			recorder := doRequest(handler, c.method, pathWithMaliciousQuery, tenantA.token, bytes.NewBufferString(c.body))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
			}
		})
	}

	// Confirm none of tenant A's attempts actually mutated tenant B's
	// data: tenant B, acting as itself, must still see zero payments.
	paymentsRecorder := doRequest(handler, http.MethodGet, "/invoices/"+invoiceB.ID+"/payments", tenantB.token, nil)
	if paymentsRecorder.Code != http.StatusOK {
		t.Fatalf("list tenant B's own payments: status %d (body: %s)", paymentsRecorder.Code, paymentsRecorder.Body.String())
	}
	var payments []json.RawMessage
	if err := json.NewDecoder(paymentsRecorder.Body).Decode(&payments); err != nil {
		t.Fatalf("decode payments response: %v", err)
	}
	if len(payments) != 0 {
		t.Errorf("expected tenant B's invoice to have 0 payments after tenant A's blocked attempts, got %d", len(payments))
	}

	// Confirm tenant A's blocked billing-address PUT attempt didn't create
	// one for tenant B either — tenant B, acting as itself, must still see
	// no billing address for its own customer.
	billingAddressRecorder := doRequest(handler, http.MethodGet, "/customers/"+customerB.ID+"/billing-address", tenantB.token, nil)
	if billingAddressRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected tenant B's customer to still have no billing address after tenant A's blocked attempts, got status %d (body: %s)", billingAddressRecorder.Code, billingAddressRecorder.Body.String())
	}

	// Cross-tenant user-management: tenant A cannot escalate/attack via
	// user creation targeting tenant B either — organisation identity for
	// POST /users comes exclusively from the actor's own authenticated
	// context, so there is no way to even address tenant B here, but a
	// deliberately supplied ?organisationId=<tenant B> query parameter
	// must still have zero effect on which organisation the new user
	// lands in.
	createRecorder := doRequest(handler, http.MethodPost, "/users?organisationId="+tenantB.organisationID, tenantA.token, bytes.NewBufferString(`{"name":"Should Belong To A","email":"cross-tenant-created@example.com","password":"`+testPassword+`"}`))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create user as tenant A: status %d (body: %s)", createRecorder.Code, createRecorder.Body.String())
	}
	var createdUser struct {
		OrganisationID string `json:"organisationId"`
	}
	if err := json.NewDecoder(createRecorder.Body).Decode(&createdUser); err != nil {
		t.Fatalf("decode created user response: %v", err)
	}
	if createdUser.OrganisationID != tenantA.organisationID {
		t.Errorf("expected the new user to belong to tenant A (%q), got %q — organisationId query parameter must be ignored", tenantA.organisationID, createdUser.OrganisationID)
	}
}
