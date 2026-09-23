package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsFrontendRoute(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/login", true},
		{"/register", true},
		{"/dashboard", true},
		{"/customers", true},
		{"/customers/11111111-1111-1111-1111-111111111111", true},
		{"/invoices/new", true},
		{"/settings/organisation", true},
		// Deliberately NOT frontend routes — these are legacy/removed
		// unversioned API paths this project's own pre-existing test
		// suite (internal/app) requires to keep 404ing (see this
		// package's own doc comment).
		{"/organisation", false},
		{"/organisations", false},
		{"/auth/login", false},
		{"/users", false},
		{"/registered", false}, // must not prefix-match "/register" without a following slash
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := IsFrontendRoute(c.path); got != c.want {
				t.Errorf("IsFrontendRoute(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestCacheControlFor(t *testing.T) {
	if got := cacheControlFor("assets/index-abc123.js"); got != "public, max-age=31536000, immutable" {
		t.Errorf("expected a hashed asset to be cached immutably, got %q", got)
	}
	if got := cacheControlFor("index.html"); got != "no-cache" {
		t.Errorf("expected index.html to be no-cache, got %q", got)
	}
}

// simulateFrontendFallback exercises Serve exactly the way it's actually
// ever invoked in production — via internal/httpx.FrontendFallback's
// interception of net/http's own NotFoundHandler — rather than calling
// Serve directly against a bare httptest.ResponseRecorder. This matters:
// NotFoundHandler pre-sets Content-Type/X-Content-Type-Options on the
// header before Serve ever runs (see prepareForFileServer's own doc
// comment), and a bare recorder wouldn't reproduce that pre-condition at
// all.
func simulateFrontendFallback(method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)

	// Let net/http's own NotFoundHandler pre-set its headers on the real
	// recorder first (Content-Type: text/plain, X-Content-Type-Options:
	// nosniff), discarding its status/body — exactly what
	// internal/httpx.FrontendFallback's interceptor does before calling
	// Serve — so Serve runs against the same pre-populated header state
	// it really runs against in production.
	http.NotFoundHandler().ServeHTTP(discardBody{recorder}, request)
	Serve(recorder, request)
	return recorder
}

// discardBody lets net/http.NotFoundHandler run to completion (setting
// its headers on the real recorder) while ignoring its WriteHeader/Write
// calls, exactly as internal/httpx.FrontendFallback's interceptor does —
// so Serve then runs against the same pre-populated header state it
// really runs against in production, without needing to import httpx
// here (which would be a reversed, undesirable package dependency).
type discardBody struct {
	http.ResponseWriter
}

func (discardBody) WriteHeader(int)             {}
func (discardBody) Write(b []byte) (int, error) { return len(b), nil }

func TestServe_FrontendRouteFallsBackToIndexHTML(t *testing.T) {
	recorder := simulateFrontendFallback(http.MethodGet, "/invoices/11111111-1111-1111-1111-111111111111")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected an HTML response, got Content-Type %q", ct)
	}
	if cc := recorder.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected no-cache, got %q", cc)
	}
}

func TestServe_RootPathServesIndexHTML(t *testing.T) {
	recorder := simulateFrontendFallback(http.MethodGet, "/")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected an HTML response, got Content-Type %q", ct)
	}
}

func TestServe_NonFrontendPathGetsExactPlainNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()
	Serve(recorder, httptest.NewRequest(http.MethodGet, "/this/is/not/a/real/route", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("expected the exact plain-text 404 content type, got %q", ct)
	}
	if body := recorder.Body.String(); body != "404 page not found\n" {
		t.Errorf("expected net/http's exact default 404 body, got %q", body)
	}
}

func TestServe_ReservedAPIPrefixNeverFallsThroughToIndexHTML(t *testing.T) {
	// Serve is only ever reached for a path net/http.ServeMux found no
	// registered pattern for (see internal/httpx.FrontendFallback) — a
	// genuinely unmatched /api/v1/... sub-path (e.g. a typo'd or
	// decommissioned endpoint) must still never receive the SPA shell.
	recorder := httptest.NewRecorder()
	Serve(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); strings.Contains(ct, "text/html") {
		t.Errorf("expected the plain 404, not HTML, got Content-Type %q", ct)
	}
}
