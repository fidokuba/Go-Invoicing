package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-invoicing/internal/metrics"
)

// TestApp_MetricsRoute_ServedWhenEnabled proves GET /metrics returns the
// standard Prometheus exposition — not this API's own JSON error
// envelope — when metrics are enabled (a non-nil *metrics.Metrics passed
// to New).
func TestApp_MetricsRoute_ServedWhenEnabled(t *testing.T) {
	handler := New(nil, testLogger, metrics.New(nil)).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if !strings.Contains(recorder.Body.String(), "go_goroutines") {
		t.Fatalf("expected the standard Go collector's output in the scrape body, got: %s", recorder.Body.String())
	}

	// Never the JSON error envelope every business API response uses —
	// section 35's explicit requirement.
	if ct := recorder.Header().Get("Content-Type"); strings.Contains(ct, "application/json") {
		t.Errorf("expected a Prometheus exposition Content-Type, got %q", ct)
	}
}

// TestApp_MetricsRoute_AbsentWhenDisabled proves that with metrics
// disabled (a nil *metrics.Metrics), GET /metrics is not merely an empty
// "disabled" response but genuinely unregistered — the same plain
// net/http.ServeMux 404 any other unregistered path gets (see
// TestApp_OrganisationsRouteRemoved for the same pattern applied to a
// removed business route). Section 32's explicit preference: absent
// route over a bespoke "metrics disabled" body.
func TestApp_MetricsRoute_AbsentWhenDisabled(t *testing.T) {
	handler := New(nil, testLogger, nil).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d for a disabled metrics endpoint, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}

	if strings.Contains(recorder.Header().Get("Content-Type"), "application/json") {
		t.Error("expected the plain net/http default 404, not this API's JSON error envelope")
	}
}

// TestApp_MetricsRoute_RequiresNoAuthentication proves GET /metrics never
// goes through AuthMiddleware — an operational scrape endpoint must not
// depend on a bearer token, exactly like /health and /health/db.
func TestApp_MetricsRoute_RequiresNoAuthentication(t *testing.T) {
	handler := New(nil, testLogger, metrics.New(nil)).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d without any Authorization header, got %d", http.StatusOK, recorder.Code)
	}
}

// TestApp_MetricsRoute_NotInRoutePatterns proves /metrics never appears
// in App.RoutePatterns() — it must stay invisible to
// TestRoutes_MatchOpenAPISpec (route_spec_test.go), since it is
// deliberately excluded from api/openapi.yaml (section 34).
func TestApp_MetricsRoute_NotInRoutePatterns(t *testing.T) {
	application := New(nil, testLogger, metrics.New(nil))
	_ = application.Handler()

	for _, r := range application.RoutePatterns() {
		if r.Path == "/metrics" {
			t.Fatalf("expected /metrics never to be recorded in RoutePatterns(), found %+v", r)
		}
	}
}

// TestApp_MetricsScrape_ExcludedFromOwnHTTPMetrics proves a /metrics
// scrape is never counted in http_requests_total itself (section 13's
// self-observation exclusion) while an ordinary request (/health) is —
// confirming section 9's decision that health checks ARE included.
func TestApp_MetricsScrape_ExcludedFromOwnHTTPMetrics(t *testing.T) {
	m := metrics.New(nil)
	handler := New(nil, testLogger, m).Handler()

	for i := 0; i < 3; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("scrape %d: expected status %d, got %d", i, http.StatusOK, recorder.Code)
		}
	}

	healthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("expected status %d for /health, got %d", http.StatusOK, healthRecorder.Code)
	}

	finalRecorder := httptest.NewRecorder()
	handler.ServeHTTP(finalRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := finalRecorder.Body.String()

	if strings.Contains(body, `route="/metrics"`) {
		t.Fatal("expected /metrics scrapes never to appear as a route label in http_requests_total")
	}
	if !strings.Contains(body, `go_invoicing_http_requests_total{method="GET",route="GET /health",status="200"} 1`) {
		t.Fatalf("expected the /health request to be counted exactly once, got:\n%s", body)
	}
}
