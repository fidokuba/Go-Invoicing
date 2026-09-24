package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// Milestone 13 Part 4: the production rate limits (defaultRateLimits),
// through the real router, auth middleware and PostgreSQL. newTestApp
// relaxes these limits for every other test; these use them as shipped.

func newDefaultLimitedApp(t *testing.T) http.Handler {
	t.Helper()

	return New(newTestPool(t), testLogger, nil).Handler()
}

func postFrom(handler http.Handler, remoteAddr, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

func assertRateLimited(t *testing.T, recorder *httptest.ResponseRecorder, retryAfter string) {
	t.Helper()

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d (body: %s)", recorder.Code, recorder.Body.String())
	}
	if code := errorCode(t, recorder); code != "rate_limited" {
		t.Errorf("expected error code rate_limited, got %q", code)
	}
	if got := recorder.Header().Get("Retry-After"); got != retryAfter {
		t.Errorf("expected Retry-After %q, got %q", retryAfter, got)
	}
}

func TestApp_RateLimit_Login(t *testing.T) {
	handler := newDefaultLimitedApp(t)
	db := newTestPool(t)
	email := "rl-login-" + uuid.NewString() + "@example.com"
	registerTenant(t, handler, db, "RL Login Org", email) // uses 1 login from doRequest's address
	const client = "192.0.2.1:1234"                       // httptest's default RemoteAddr

	right := `{"email":"` + email + `","password":"` + testPassword + `"}`
	wrong := `{"email":"` + email + `","password":"not the password"}`

	// Below the limit, authentication behaves exactly as before.
	for i := 0; i < 9; i++ {
		body, want := wrong, http.StatusUnauthorized
		if i%3 == 0 {
			body, want = right, http.StatusOK
		}
		if recorder := postFrom(handler, client, "/api/v1/auth/login", body); recorder.Code != want {
			t.Fatalf("attempt %d under the limit: expected %d, got %d", i+1, want, recorder.Code)
		}
	}

	// The 11th attempt (burst 10) is limited — right password or not.
	assertRateLimited(t, postFrom(handler, client, "/api/v1/auth/login", right), "6")

	// Another client address has its own, untouched bucket.
	if recorder := postFrom(handler, "198.51.100.9:4000", "/api/v1/auth/login", right); recorder.Code != http.StatusOK {
		t.Errorf("expected a different client address to log in normally, got %d", recorder.Code)
	}
}

func TestApp_RateLimit_Register(t *testing.T) {
	handler := newDefaultLimitedApp(t)
	db := newTestPool(t)
	const client = "203.0.113.50:5000"

	registration := func() string {
		email := "rl-register-" + uuid.NewString() + "@example.com"
		return `{"organisation":{"name":"RL Register Org"},"user":{"name":"Admin","email":"` + email + `","password":"` + testPassword + `"}}`
	}

	for i := 0; i < 5; i++ {
		recorder := postFrom(handler, client, "/api/v1/register", registration())
		if recorder.Code != http.StatusCreated {
			t.Fatalf("registration %d under the limit: expected 201, got %d (body: %s)", i+1, recorder.Code, recorder.Body.String())
		}
		var response struct {
			Organisation struct {
				ID string `json:"id"`
			} `json:"organisation"`
		}
		_ = json.Unmarshal(recorder.Body.Bytes(), &response)
		t.Cleanup(func() { cleanupOrganisation(db, response.Organisation.ID) })
	}

	assertRateLimited(t, postFrom(handler, client, "/api/v1/register", registration()), "120")
}

func TestApp_RateLimit_PDFIsPerUser(t *testing.T) {
	handler := newDefaultLimitedApp(t)
	db := newTestPool(t)
	owner := registerTenant(t, handler, db, "RL PDF Org", "rl-pdf-"+uuid.NewString()+"@example.com")
	_, colleague := createAndLoginUser(t, handler, owner.token, "Colleague", "rl-pdf-colleague-"+uuid.NewString()+"@example.com", "user")
	invoiceID := createSentInvoice(t, handler, owner.token)
	path := "/api/v1/invoices/" + invoiceID + "/pdf"

	// Unauthenticated: 401 from RequireAuth, never reaching (or draining) the limiter.
	for i := 0; i < 25; i++ {
		if recorder := doRequest(handler, http.MethodGet, path, "", nil); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated: expected 401, got %d", recorder.Code)
		}
	}

	for i := 0; i < 20; i++ {
		if recorder := doRequest(handler, http.MethodGet, path, owner.token, nil); recorder.Code != http.StatusOK {
			t.Fatalf("PDF %d under the limit: expected 200, got %d", i+1, recorder.Code)
		}
	}
	assertRateLimited(t, doRequest(handler, http.MethodGet, path, owner.token, nil), "2")

	// Same client address, different user: its own bucket.
	if recorder := doRequest(handler, http.MethodGet, path, colleague, nil); recorder.Code != http.StatusOK {
		t.Errorf("expected another user's PDF request to succeed, got %d", recorder.Code)
	}
}
