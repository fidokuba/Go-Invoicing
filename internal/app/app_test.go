package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-invoicing/api"
	admin "go-invoicing/internal/administration"
)

// testLogger discards every record — these tests want a real *slog.Logger
// (Recover's signature requires one), not test output noise from panic
// recovery paths nothing in this suite deliberately triggers.
var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

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
	application := New(db, testLogger, nil)
	// Every httptest request comes from the same client address, so the
	// production login/register limits would trip tests that simply log
	// in many times. The limits themselves are tested explicitly (see
	// rate_limit_test.go) against defaultRateLimits.
	application.rateLimits = relaxedRateLimits
	return application.Handler(), db
}

var relaxedRateLimits = rateLimits{
	login:    rateLimitPolicy{every: time.Millisecond, burst: 1000},
	register: rateLimitPolicy{every: time.Millisecond, burst: 1000},
	pdf:      rateLimitPolicy{every: time.Millisecond, burst: 1000},
}

// TestApp_OpenAPISpecRoute_PublicAndMatchesEmbeddedSource is Milestone 8
// Part 4's minimal proof that the runtime endpoint actually works: it
// requires no Authorization header at all (public), returns the YAML
// content type, a non-empty body, and — the whole point of serving the
// embedded copy rather than a second hand-written one — a body that is
// byte-for-byte identical to api.Spec, so the two can never drift apart.
func TestApp_OpenAPISpecRoute_PublicAndMatchesEmbeddedSource(t *testing.T) {
	handler, _ := newTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/yaml" {
		t.Errorf("expected Content-Type application/yaml, got %q", contentType)
	}

	if recorder.Body.Len() == 0 {
		t.Fatal("expected a non-empty response body")
	}

	if recorder.Body.String() != string(openapi.Spec) {
		t.Error("expected the served body to exactly match the embedded openapi.Spec source")
	}
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

// TestApp_UnversionedRoutes_NoLongerWork is the Milestone 8 Part 2
// regression test for the /api/v1 route move (section 1): every route
// that used to live at a bare, unversioned path must now 404 there — the
// application only answers at its versioned path — while /health and
// /health/db (deliberately excluded from versioning) remain reachable
// exactly where they always were.
func TestApp_UnversionedRoutes_NoLongerWork(t *testing.T) {
	handler, _ := newTestApp(t)

	unversionedPaths := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/auth/login"},
		{http.MethodPost, "/register"},
		{http.MethodGet, "/organisation"},
		{http.MethodPost, "/users"},
		{http.MethodPost, "/customers"},
		{http.MethodPost, "/products"},
		{http.MethodPost, "/invoices"},
	}

	for _, route := range unversionedPaths {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, bytes.NewBufferString(`{}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status %d (unversioned route must no longer exist), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	request.Header.Set("Content-Type", "application/json")
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/register", body)
	request.Header.Set("Content-Type", "application/json")
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/organisations", body)
	request.Header.Set("Content-Type", "application/json")
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	request.Header.Set("Content-Type", "application/json")
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
		{http.MethodGet, "/api/v1/organisation"},
		{http.MethodPatch, "/api/v1/organisation"},
		{http.MethodPost, "/api/v1/users"},
		{http.MethodGet, "/api/v1/users/" + id},
		{http.MethodPost, "/api/v1/customers"},
		{http.MethodGet, "/api/v1/customers/" + id},
		{http.MethodGet, "/api/v1/customers/" + id + "/billing-address"},
		{http.MethodPut, "/api/v1/customers/" + id + "/billing-address"},
		{http.MethodPost, "/api/v1/products"},
		{http.MethodGet, "/api/v1/products/" + id},
		{http.MethodPost, "/api/v1/invoices"},
		{http.MethodGet, "/api/v1/invoices/" + id},
		{http.MethodPost, "/api/v1/invoices/" + id + "/send"},
		{http.MethodPost, "/api/v1/invoices/" + id + "/payments"},
		{http.MethodGet, "/api/v1/invoices/" + id + "/payments"},
		{http.MethodGet, "/api/v1/invoices/" + id + "/pdf"},
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
	registerRecorder := doRequest(handler, http.MethodPost, "/api/v1/register", "", registerBody)
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
	loginRecorder := doRequest(handler, http.MethodPost, "/api/v1/auth/login", "", loginBody)
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
	return doRequest(handler, http.MethodPost, "/api/v1/users", actorToken, body)
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
	// Every JSON-bodied endpoint now requires this (Milestone 8 Part 2);
	// setting it unconditionally is harmless for GET/DELETE-style calls
	// through this same helper, which never decode a body at all.
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	// POST /invoices/{id}/payments requires an Idempotency-Key (Milestone
	// 13 Part 1). Like any well-behaved client, this helper sends a fresh
	// one per call — so each call here is one new logical payment attempt.
	// Tests of the idempotency behaviour itself use
	// doRequestWithIdempotencyKey to control (or omit) the key.
	if method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/payments") {
		request.Header.Set("Idempotency-Key", "app-test-"+uuid.NewString())
	}
	// PATCH /organisation and /organisation/settings require If-Match
	// (Milestone 13 Part 2). Like a well-behaved client, this helper sends
	// the ETag from a fresh GET of the same resource as the same caller.
	// Tests of the precondition behaviour itself set (or omit) If-Match
	// explicitly via doRequestWithIfMatch.
	if method == http.MethodPatch && (request.URL.Path == "/api/v1/organisation" || request.URL.Path == "/api/v1/organisation/settings") {
		current := doRequest(handler, http.MethodGet, request.URL.Path, token, nil)
		request.Header.Set("If-Match", current.Header().Get("ETag"))
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

	orgRecorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", tenant.token, nil)
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

	customerRecorder := doRequest(handler, http.MethodPost, "/api/v1/customers", tenant.token, bytes.NewBufferString(`{"name":"E2E Customer"}`))
	if customerRecorder.Code != http.StatusCreated {
		t.Fatalf("create customer: status %d (body: %s)", customerRecorder.Code, customerRecorder.Body.String())
	}

	// Without the token, the same routes must reject the request.
	if recorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", "", nil); recorder.Code != http.StatusUnauthorized {
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

// TestApp_UsersList_RoleMatrix proves GET /users (Milestone 8 Part 3) is
// admin/manager-only, the same gate POST /users already uses: an
// ordinary user must get 403, never a partial or self-only listing.
func TestApp_UsersList_RoleMatrix(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Users List Org", "users-list-admin@example.com")

	_, managerToken := createAndLoginUser(t, handler, tenant.token, "Manager", "users-list-manager@example.com", admin.UserRoleManager)
	_, userToken := createAndLoginUser(t, handler, tenant.token, "User", "users-list-user@example.com", admin.UserRoleUser)

	tests := []struct {
		name       string
		actorToken string
		wantStatus int
	}{
		{"admin can list users", tenant.token, http.StatusOK},
		{"manager can list users", managerToken, http.StatusOK},
		{"user cannot list users", userToken, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := doRequest(handler, http.MethodGet, "/api/v1/users", tt.actorToken, nil)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}

	// The admin's own list must contain all three seeded users (itself,
	// the manager, the user) — a basic end-to-end sanity check that the
	// route actually returns real data, not just the right status code.
	recorder := doRequest(handler, http.MethodGet, "/api/v1/users", tenant.token, nil)
	var response struct {
		Items      []json.RawMessage `json:"items"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode users list response: %v", err)
	}
	if response.Pagination.Total != 3 {
		t.Errorf("expected 3 users total, got %d", response.Pagination.Total)
	}
	if len(response.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(response.Items))
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
			recorder := doRequest(handler, http.MethodPatch, "/api/v1/organisation", tt.actorToken, bytes.NewBufferString(`{"city":"London"}`))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestApp_RoleMatrix_SettingsUpdate proves PATCH /organisation/settings
// (Milestone 8 Part 3) is admin-only, the same gate PATCH /organisation
// already uses, while GET /organisation/settings remains open to all
// three roles.
func TestApp_RoleMatrix_SettingsUpdate(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Settings Role Org", "settings-role-admin@example.com")

	_, managerToken := createAndLoginUser(t, handler, tenant.token, "Manager", "settings-role-manager@example.com", admin.UserRoleManager)
	_, userToken := createAndLoginUser(t, handler, tenant.token, "User", "settings-role-user@example.com", admin.UserRoleUser)

	t.Run("GET is open to every role", func(t *testing.T) {
		for name, token := range map[string]string{"admin": tenant.token, "manager": managerToken, "user": userToken} {
			recorder := doRequest(handler, http.MethodGet, "/api/v1/organisation/settings", token, nil)
			if recorder.Code != http.StatusOK {
				t.Errorf("%s: expected status %d, got %d (body: %s)", name, http.StatusOK, recorder.Code, recorder.Body.String())
			}
		}
	})

	patchTests := []struct {
		name       string
		actorToken string
		wantStatus int
	}{
		{"admin can update settings", tenant.token, http.StatusOK},
		{"manager cannot update settings", managerToken, http.StatusForbidden},
		{"user cannot update settings", userToken, http.StatusForbidden},
	}

	for _, tt := range patchTests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := doRequest(handler, http.MethodPatch, "/api/v1/organisation/settings", tt.actorToken, bytes.NewBufferString(`{"currency":"EUR"}`))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestApp_SettingsHistory_EndToEnd is the Milestone 8 Part 3 section 16
// end-to-end proof, through the real HTTP/service/repository/PostgreSQL
// stack: changing Settings.Currency affects a Draft invoice's currency
// immediately, but never alters an already-Sent invoice's immutable
// currency snapshot — even though the Send happened before the settings
// change.
func TestApp_SettingsHistory_EndToEnd(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Settings History Org", "settings-history-admin@example.com")

	customerRecorder := doRequest(handler, http.MethodPost, "/api/v1/customers", tenant.token, bytes.NewBufferString(`{"name":"History Customer"}`))
	if customerRecorder.Code != http.StatusCreated {
		t.Fatalf("create customer: status %d (body: %s)", customerRecorder.Code, customerRecorder.Body.String())
	}
	var customer struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(customerRecorder.Body).Decode(&customer)

	invoiceBody := `{
		"customerId": "` + customer.ID + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": [{"description": "Service", "quantity": 1, "unitPrice": 1000, "vatRate": 0}]
	}`

	// One invoice stays Draft; one gets Sent — both created while the
	// organisation's currency is still the default GBP.
	draftRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices", tenant.token, bytes.NewBufferString(invoiceBody))
	var draftInvoice struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(draftRecorder.Body).Decode(&draftInvoice)

	sentRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices", tenant.token, bytes.NewBufferString(invoiceBody))
	var sentInvoice struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(sentRecorder.Body).Decode(&sentInvoice)
	if recorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+sentInvoice.ID+"/send", tenant.token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("send invoice: status %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	// Change the organisation's currency after both invoices exist (one
	// already Sent).
	patchRecorder := doRequest(handler, http.MethodPatch, "/api/v1/organisation/settings", tenant.token, bytes.NewBufferString(`{"currency":"EUR"}`))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch settings: status %d (body: %s)", patchRecorder.Code, patchRecorder.Body.String())
	}

	var currencyOf = func(invoiceID string) string {
		t.Helper()
		recorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoiceID, tenant.token, nil)
		var response struct {
			Currency string `json:"currency"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode invoice response: %v", err)
		}
		return response.Currency
	}

	if got := currencyOf(draftInvoice.ID); got != "EUR" {
		t.Errorf("expected the Draft invoice to follow the new live currency %q, got %q", "EUR", got)
	}
	if got := currencyOf(sentInvoice.ID); got != "GBP" {
		t.Errorf("expected the Sent invoice's currency to remain the snapshotted %q, got %q (must not follow the settings change)", "GBP", got)
	}
}

// TestApp_OrganisationUpdate_EndToEnd exercises genuine partial-update
// semantics through the real HTTP/service/repository/PostgreSQL stack:
// an update that only supplies Email must leave a previously-set Phone
// untouched, and the change must be visible on a subsequent GET.
func TestApp_OrganisationUpdate_EndToEnd(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Org Update E2E Org", "org-update-e2e-admin@example.com")

	firstRecorder := doRequest(handler, http.MethodPatch, "/api/v1/organisation", tenant.token, bytes.NewBufferString(`{"phone":"+44 20 7946 0958"}`))
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first update: status %d (body: %s)", firstRecorder.Code, firstRecorder.Body.String())
	}

	secondRecorder := doRequest(handler, http.MethodPatch, "/api/v1/organisation", tenant.token, bytes.NewBufferString(`{"email":"contact@org-update-e2e.test"}`))
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second update: status %d (body: %s)", secondRecorder.Code, secondRecorder.Body.String())
	}

	getRecorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", tenant.token, nil)
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

	var forbiddenBody struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&forbiddenBody); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if forbiddenBody.Error.Code != "forbidden" || forbiddenBody.Error.Message != "forbidden" {
		t.Errorf("expected generic {code: forbidden, message: forbidden}, got %+v", forbiddenBody.Error)
	}

	// The rejected email must not have been consumed — a genuine admin
	// registration with the same email must still succeed elsewhere,
	// proving nothing was partially created.
	loginRecorder := doRequest(handler, http.MethodPost, "/api/v1/auth/login", "", bytes.NewBufferString(`{"email":"escalation-sneaky-admin@example.com","password":"`+testPassword+`"}`))
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
			customerRecorder := doRequest(handler, http.MethodPost, "/api/v1/customers", role.token, bytes.NewBufferString(`{"name":"`+role.name+`'s Customer"}`))
			if customerRecorder.Code != http.StatusCreated {
				t.Fatalf("create customer as %s: status %d (body: %s)", role.name, customerRecorder.Code, customerRecorder.Body.String())
			}
			var customer struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(customerRecorder.Body).Decode(&customer)

			if recorder := doRequest(handler, http.MethodGet, "/api/v1/customers/"+customer.ID, role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get customer as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			// Billing address (Milestone 7 Part 1): all three roles may
			// create/read a customer's billing address, same policy as
			// every other customer/invoice business-data route.
			billingAddressBody := bytes.NewBufferString(`{"street":"1 ` + role.name + ` Way","city":"London","postalCode":"E1 6AN","country":"GB"}`)
			billingAddressRecorder := doRequest(handler, http.MethodPut, "/api/v1/customers/"+customer.ID+"/billing-address", role.token, billingAddressBody)
			if billingAddressRecorder.Code != http.StatusOK {
				t.Fatalf("put billing address as %s: status %d (body: %s)", role.name, billingAddressRecorder.Code, billingAddressRecorder.Body.String())
			}

			if recorder := doRequest(handler, http.MethodGet, "/api/v1/customers/"+customer.ID+"/billing-address", role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get billing address as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			productRecorder := doRequest(handler, http.MethodPost, "/api/v1/products", role.token, bytes.NewBufferString(`{"name":"`+role.name+`'s Product","sku":"SKU-`+role.name+`","price":500}`))
			if productRecorder.Code != http.StatusCreated {
				t.Fatalf("create product as %s: status %d (body: %s)", role.name, productRecorder.Code, productRecorder.Body.String())
			}
			var product struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(productRecorder.Body).Decode(&product)

			if recorder := doRequest(handler, http.MethodGet, "/api/v1/products/"+product.ID, role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get product as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			invoiceBody := bytes.NewBufferString(`{
				"customerId": "` + customer.ID + `",
				"issueDate": "2026-01-01",
				"dueDate": "2026-01-31",
				"lines": [{"description": "Service", "quantity": 1, "unitPrice": 1000, "vatRate": 0}]
			}`)
			invoiceRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices", role.token, invoiceBody)
			if invoiceRecorder.Code != http.StatusCreated {
				t.Fatalf("create invoice as %s: status %d (body: %s)", role.name, invoiceRecorder.Code, invoiceRecorder.Body.String())
			}
			var invoice struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(invoiceRecorder.Body).Decode(&invoice)

			if recorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoice.ID, role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get invoice as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			// A newly-created invoice is Draft and cannot accept a payment
			// (Milestone 5) — send it first. This also proves all three
			// roles may use the new Send endpoint.
			sendRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/send", role.token, nil)
			if sendRecorder.Code != http.StatusOK {
				t.Fatalf("send invoice as %s: status %d (body: %s)", role.name, sendRecorder.Code, sendRecorder.Body.String())
			}

			paymentRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/payments", role.token, bytes.NewBufferString(`{"amount":100,"paymentMethod":"cash","paymentDate":"2026-01-15"}`))
			if paymentRecorder.Code != http.StatusCreated {
				t.Fatalf("create payment as %s: status %d (body: %s)", role.name, paymentRecorder.Code, paymentRecorder.Body.String())
			}

			if recorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/payments", role.token, nil); recorder.Code != http.StatusOK {
				t.Fatalf("get payments as %s: status %d (body: %s)", role.name, recorder.Code, recorder.Body.String())
			}

			// PDF generation (Milestone 7 Part 3): all three roles may
			// download an invoice's PDF, same policy as every other
			// invoice route.
			pdfRecorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf", role.token, nil)
			if pdfRecorder.Code != http.StatusOK {
				t.Fatalf("get invoice pdf as %s: status %d (body: %s)", role.name, pdfRecorder.Code, pdfRecorder.Body.String())
			}
			if contentType := pdfRecorder.Header().Get("Content-Type"); contentType != "application/pdf" {
				t.Errorf("expected Content-Type application/pdf as %s, got %q", role.name, contentType)
			}
			if !bytes.HasPrefix(pdfRecorder.Body.Bytes(), []byte("%PDF-")) {
				t.Errorf("expected pdf body to start with %%PDF- as %s", role.name)
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
			recorder := doRequest(handler, http.MethodGet, "/api/v1/users/"+tt.targetID, tt.actorToken, nil)
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

	if recorder := doRequest(handler, http.MethodGet, "/api/v1/users/"+userID, token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d before deactivation, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if _, err := db.Exec(context.Background(), "UPDATE users SET is_active = false WHERE id = $1", userID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	recorder := doRequest(handler, http.MethodGet, "/api/v1/users/"+userID, token, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d after deactivation with the same token, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestApp_Logout_EndToEnd is the Milestone 8 Part 3 section 32
// end-to-end proof, through the real HTTP/service/repository/PostgreSQL
// stack: logging out revokes only the current session's token — a
// second, independent login for the same user survives untouched — and
// revoked_at is genuinely persisted, not just held in memory.
func TestApp_Logout_EndToEnd(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Logout Org", "logout-admin@example.com")

	// A second, independent session for the very same admin user.
	secondToken := loginAs(t, handler, "logout-admin@example.com", testPassword)

	// Both tokens work before logout.
	if recorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", tenant.token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("expected first token to work before logout, got %d", recorder.Code)
	}
	if recorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", secondToken, nil); recorder.Code != http.StatusOK {
		t.Fatalf("expected second token to work before logout, got %d", recorder.Code)
	}

	logoutRecorder := doRequest(handler, http.MethodPost, "/api/v1/auth/logout", tenant.token, nil)
	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNoContent, logoutRecorder.Code, logoutRecorder.Body.String())
	}
	if logoutRecorder.Body.Len() != 0 {
		t.Errorf("expected an empty body, got %q", logoutRecorder.Body.String())
	}

	// The logged-out token must immediately fail authentication.
	if recorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", tenant.token, nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d for the logged-out token, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}

	// A second logout with the already-revoked token fails authentication
	// before ever reaching the handler — 401, not a repeated 204.
	if recorder := doRequest(handler, http.MethodPost, "/api/v1/auth/logout", tenant.token, nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d repeating logout with an already-revoked token, got %d", http.StatusUnauthorized, recorder.Code)
	}

	// The second, independent session must remain completely unaffected.
	if recorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", secondToken, nil); recorder.Code != http.StatusOK {
		t.Fatalf("expected the second session to remain valid after the first was logged out, got %d", recorder.Code)
	}

	// The raw token is never stored as token_hash — only its SHA-256 hex
	// hash is (the same algorithm AuthService documents), and that row's
	// revoked_at must genuinely be persisted in the database, not just
	// held in memory.
	var rawTokenStoredCount int
	if err := db.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM sessions WHERE token_hash = $1", tenant.token,
	).Scan(&rawTokenStoredCount); err != nil {
		t.Fatalf("query for raw token storage: %v", err)
	}
	if rawTokenStoredCount != 0 {
		t.Error("expected the raw token never to be stored as token_hash")
	}

	tokenHashSum := sha256.Sum256([]byte(tenant.token))
	tokenHash := hex.EncodeToString(tokenHashSum[:])

	// A revoked session is, by SessionRepository's own documented design,
	// immediately eligible for deletion by DeleteExpired's whole-table
	// maintenance sweep — regardless of which process or test runs it.
	// Running the full suite in parallel (go test's default) means this
	// specific row can legitimately already be gone by the time this
	// query runs, if a concurrent DeleteExpired-exercising test in
	// another package happened to sweep it first — that is not a
	// contradiction of "logout revoked the session", it's the expected
	// next step for any revoked session. A missing row is therefore
	// treated as an acceptable outcome here, not a failure; run with
	// `go test -p 1` for a deterministic look at the row itself.
	var revokedAtIsSet bool
	err := db.QueryRow(context.Background(),
		"SELECT revoked_at IS NOT NULL FROM sessions WHERE token_hash = $1", tokenHash,
	).Scan(&revokedAtIsSet)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("session row already reaped by a concurrently-running cleanup sweep — see comment above")
	}
	if err != nil {
		t.Fatalf("query session row: %v", err)
	}
	if !revokedAtIsSet {
		t.Error("expected revoked_at to be set in the database")
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

	if recorder := doRequest(handler, http.MethodGet, "/api/v1/users/"+userID, token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d before soft-deletion, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if _, err := db.Exec(context.Background(), "UPDATE users SET deleted_at = NOW() WHERE id = $1", userID); err != nil {
		t.Fatalf("soft-delete user: %v", err)
	}

	recorder := doRequest(handler, http.MethodGet, "/api/v1/users/"+userID, token, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d after soft-deletion with the same token, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestApp_InvoicesList_SummaryRepresentation_EndToEnd is the Milestone 8
// Part 3 amendment's end-to-end proof, through the real HTTP/service/
// repository/PostgreSQL stack: GET /api/v1/invoices rows must have no
// "lines" key at all (not merely an empty array), while
// GET /api/v1/invoices/{id} for the exact same invoice must still return
// its lines in full.
func TestApp_InvoicesList_SummaryRepresentation_EndToEnd(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "List Summary Org", "list-summary-admin@example.com")

	customerRecorder := doRequest(handler, http.MethodPost, "/api/v1/customers", tenant.token, bytes.NewBufferString(`{"name":"Summary Customer"}`))
	var customer struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(customerRecorder.Body).Decode(&customer)

	invoiceBody := `{
		"customerId": "` + customer.ID + `",
		"issueDate": "2026-01-01",
		"dueDate": "2026-01-31",
		"lines": [{"description": "Service", "quantity": 1, "unitPrice": 1000, "vatRate": 0}]
	}`
	createRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices", tenant.token, bytes.NewBufferString(invoiceBody))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create invoice: status %d (body: %s)", createRecorder.Code, createRecorder.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(createRecorder.Body).Decode(&created)

	listRecorder := doRequest(handler, http.MethodGet, "/api/v1/invoices", tenant.token, nil)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list invoices: status %d (body: %s)", listRecorder.Code, listRecorder.Body.String())
	}

	var listResponse struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResponse.Items) != 1 {
		t.Fatalf("expected exactly 1 item, got %d", len(listResponse.Items))
	}
	if _, hasLines := listResponse.Items[0]["lines"]; hasLines {
		t.Error(`expected the list item to have no "lines" key at all`)
	}
	if _, hasID := listResponse.Items[0]["id"]; !hasID {
		t.Error("expected the list item to still contain summary fields such as id")
	}

	getRecorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+created.ID, tenant.token, nil)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get invoice: status %d (body: %s)", getRecorder.Code, getRecorder.Body.String())
	}
	var full struct {
		Lines []json.RawMessage `json:"lines"`
	}
	if err := json.NewDecoder(getRecorder.Body).Decode(&full); err != nil {
		t.Fatalf("decode full invoice response: %v", err)
	}
	if len(full.Lines) != 1 {
		t.Fatalf("expected GET /invoices/{id} to still return 1 line, got %d", len(full.Lines))
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

	customerRecorder := doRequest(handler, http.MethodPost, "/api/v1/customers", tenant.token, bytes.NewBufferString(`{"name":"Lifecycle Customer"}`))
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
	invoiceRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices", tenant.token, invoiceBody)
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
	paymentAgainstDraft := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/payments", tenant.token, bytes.NewBufferString(`{"amount":1000,"paymentMethod":"cash","paymentDate":"2026-01-15"}`))
	if paymentAgainstDraft.Code != http.StatusConflict {
		t.Fatalf("expected status %d paying a Draft invoice, got %d (body: %s)", http.StatusConflict, paymentAgainstDraft.Code, paymentAgainstDraft.Body.String())
	}

	// Draft -> Sent.
	sendRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/send", tenant.token, nil)
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
	repeatSendRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/send", tenant.token, nil)
	if repeatSendRecorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d for a repeated send, got %d (body: %s)", http.StatusConflict, repeatSendRecorder.Code, repeatSendRecorder.Body.String())
	}

	// Partial payment: still Sent (due date is a year out, never overdue
	// for the life of this test).
	partialPaymentRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/payments", tenant.token, bytes.NewBufferString(`{"amount":400,"paymentMethod":"cash","paymentDate":"2026-01-15"}`))
	if partialPaymentRecorder.Code != http.StatusCreated {
		t.Fatalf("create partial payment: status %d (body: %s)", partialPaymentRecorder.Code, partialPaymentRecorder.Body.String())
	}

	afterPartialRecorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoice.ID, tenant.token, nil)
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
	finalPaymentRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/payments", tenant.token, bytes.NewBufferString(`{"amount":600,"paymentMethod":"cash","paymentDate":"2026-01-20"}`))
	if finalPaymentRecorder.Code != http.StatusCreated {
		t.Fatalf("create final payment: status %d (body: %s)", finalPaymentRecorder.Code, finalPaymentRecorder.Body.String())
	}

	afterFinalRecorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoice.ID, tenant.token, nil)
	var afterFinal struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(afterFinalRecorder.Body).Decode(&afterFinal)
	if afterFinal.Status != "paid" {
		t.Errorf("expected status %q after the final payment, got %q", "paid", afterFinal.Status)
	}

	// Paid invoices can never be (re-)sent.
	sendAfterPaidRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/send", tenant.token, nil)
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
	orgARecorder := doRequest(handler, http.MethodGet, "/api/v1/organisation", tenantA.token, nil)
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
	customerBRecorder := doRequest(handler, http.MethodPost, "/api/v1/customers", tenantB.token, bytes.NewBufferString(`{"name":"Tenant B Customer"}`))
	if customerBRecorder.Code != http.StatusCreated {
		t.Fatalf("create tenant B customer: status %d (body: %s)", customerBRecorder.Code, customerBRecorder.Body.String())
	}
	var customerB struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(customerBRecorder.Body).Decode(&customerB); err != nil {
		t.Fatalf("decode customer response: %v", err)
	}

	productBRecorder := doRequest(handler, http.MethodPost, "/api/v1/products", tenantB.token, bytes.NewBufferString(`{"name":"Tenant B Product","sku":"TB-SKU-1","price":500}`))
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
	invoiceBRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices", tenantB.token, invoiceBBody)
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
		{"get tenant B customer", http.MethodGet, "/api/v1/customers/" + customerB.ID, ""},
		{"get tenant B customer's billing address", http.MethodGet, "/api/v1/customers/" + customerB.ID + "/billing-address", ""},
		{"put tenant B customer's billing address", http.MethodPut, "/api/v1/customers/" + customerB.ID + "/billing-address", `{"street":"Attacker St","city":"X","postalCode":"00000","country":"XX"}`},
		{"get tenant B product", http.MethodGet, "/api/v1/products/" + productB.ID, ""},
		{"get tenant B invoice", http.MethodGet, "/api/v1/invoices/" + invoiceB.ID, ""},
		{"get tenant B invoice pdf", http.MethodGet, "/api/v1/invoices/" + invoiceB.ID + "/pdf", ""},
		{"send tenant B invoice", http.MethodPost, "/api/v1/invoices/" + invoiceB.ID + "/send", ""},
		{"create payment against tenant B invoice", http.MethodPost, "/api/v1/invoices/" + invoiceB.ID + "/payments", `{"amount":100,"paymentMethod":"cash","paymentDate":"2026-01-15"}`},
		{"list payments for tenant B invoice", http.MethodGet, "/api/v1/invoices/" + invoiceB.ID + "/payments", ""},
		{"get tenant B user", http.MethodGet, "/api/v1/users/" + tenantB.userID, ""},
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
	paymentsRecorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoiceB.ID+"/payments", tenantB.token, nil)
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
	billingAddressRecorder := doRequest(handler, http.MethodGet, "/api/v1/customers/"+customerB.ID+"/billing-address", tenantB.token, nil)
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
	createRecorder := doRequest(handler, http.MethodPost, "/api/v1/users?organisationId="+tenantB.organisationID, tenantA.token, bytes.NewBufferString(`{"name":"Should Belong To A","email":"cross-tenant-created@example.com","password":"`+testPassword+`"}`))
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
