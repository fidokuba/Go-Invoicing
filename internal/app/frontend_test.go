package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestApp_FrontendDeepLink proves a client-side route like
// /invoices/<id> — refreshed or linked to directly, not navigated to
// from within the SPA — renders the frontend shell rather than a Go 404
// (Milestone 12 sections 31/46).
func TestApp_FrontendDeepLink(t *testing.T) {
	handler, _ := newTestApp(t)

	for _, path := range []string{"/", "/login", "/invoices", "/invoices/11111111-1111-1111-1111-111111111111", "/customers", "/products", "/settings"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
			}
			if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
				t.Errorf("expected an HTML response, got Content-Type %q", ct)
			}
		})
	}
}

// TestApp_UnmatchedAPIPathNeverReturnsFrontendHTML is section 31's core
// requirement: a genuinely nonexistent path under /api/v1, /health, or
// /metrics must never fall through to the SPA shell.
func TestApp_UnmatchedAPIPathNeverReturnsFrontendHTML(t *testing.T) {
	handler, _ := newTestApp(t)

	for _, path := range []string{"/api/v1/does-not-exist", "/api/v1/invoices/not-a-real-sub-route/extra"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
			}
			if ct := recorder.Header().Get("Content-Type"); strings.Contains(ct, "text/html") {
				t.Errorf("expected a non-HTML 404, got Content-Type %q (body: %s)", ct, recorder.Body.String())
			}
		})
	}
}

// TestApp_FrontendFallback_DoesNotAffectExistingRouteContract is a
// direct regression guard for the exact bug this milestone's frontend
// integration introduced and then fixed: adding frontend serving must
// never change the status of any EXISTING route's wrong-method request
// from 404 to 405, or vice versa. This duplicates a slice of
// TestApp_UnversionedRoutes_NoLongerWork deliberately — as a narrowly-
// scoped, purpose-named test that documents exactly why it matters here.
func TestApp_FrontendFallback_DoesNotAffectExistingRouteContract(t *testing.T) {
	handler, _ := newTestApp(t)

	cases := []struct {
		method string
		path   string
		want   int
	}{
		// Removed/unversioned legacy paths: still a plain 404, not 405
		// and not the SPA shell — even though "/customers"/"/invoices"
		// are real frontend GET routes at the exact same literal path.
		{http.MethodPost, "/customers", http.StatusNotFound},
		{http.MethodPost, "/invoices", http.StatusNotFound},
		{http.MethodPost, "/register", http.StatusNotFound},
		{http.MethodGet, "/organisation", http.StatusNotFound},
		// A real, registered API route's own method-mismatch 405 must be
		// completely unaffected by frontend serving.
		{http.MethodDelete, "/api/v1/customers", http.StatusMethodNotAllowed},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(c.method, c.path, nil))

			if recorder.Code != c.want {
				t.Fatalf("expected status %d, got %d (body: %s)", c.want, recorder.Code, recorder.Body.String())
			}
		})
	}
}
