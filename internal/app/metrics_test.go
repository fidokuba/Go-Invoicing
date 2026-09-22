package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	if !strings.Contains(body, `go_invoicing_http_requests_total{method="GET",route="/health",status="200"} 1`) {
		t.Fatalf("expected the /health request to be counted exactly once, got:\n%s", body)
	}
}

// --- Milestone 10 Part 5: adversarial hardening tests ---

// TestApp_MetricsRoute_NonGETMethodGets405WithAllowHeader proves /metrics
// gets exactly the same net/http.ServeMux method-matching treatment as
// every other route in this application: registered as "GET /metrics"
// only, so a non-GET/HEAD request against it is ServeMux's own router-
// generated 405 (converted to the standard JSON envelope by
// WrapMethodNotAllowed, same as any other route — see that middleware's
// own doc comment) with a correct Allow header — no bespoke behaviour
// invented for this one endpoint. Allow lists "GET, HEAD" rather than
// just "GET": net/http.ServeMux (Go 1.22+) automatically serves HEAD for
// any GET-registered pattern, so HEAD is genuinely allowed here too —
// this is standard library behaviour, not anything this application
// added.
func TestApp_MetricsRoute_NonGETMethodGets405WithAllowHeader(t *testing.T) {
	handler := New(nil, testLogger, metrics.New(nil)).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/metrics", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusMethodNotAllowed, recorder.Code, recorder.Body.String())
	}
	if allow := recorder.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("expected Allow: GET, HEAD, got %q", allow)
	}
	if ct := recorder.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected the standard JSON error envelope Content-Type, got %q", ct)
	}
}

// TestApp_MetricsExposition_NeverContainsAdversarialMarkers is section
// 21's privacy scan: it drives distinctive markers through every input
// surface a request offers (path, query string, Authorization header,
// Cookie header, request body) against a nil-pool App — enough to
// exercise routing, auth rejection, and validation failure paths without
// a live database — then scrapes /metrics and asserts none of those
// markers, nor the request paths/queries themselves, ever appear in the
// exposition body. A DB-backed extension of this same property (real
// login/customer data) is TestApp_MetricsExposition_RealRequestData_
// NeverContainsBusinessData below, gated on DATABASE_URL.
func TestApp_MetricsExposition_NeverContainsAdversarialMarkers(t *testing.T) {
	m := metrics.New(nil)
	handler := New(nil, testLogger, m).Handler()

	const (
		authMarker  = "Bearer ATTACKER-BEARER-TOKEN-MARKER"
		cookieMark  = "session=ATTACKER-COOKIE-MARKER"
		bodyMarker  = "ATTACKER-BODY-MARKER-emailmarker@example.test"
		pathMarker  = "ATTACKER-PATH-MARKER-550e8400"
		queryMarker = "ATTACKER-QUERY-MARKER-deadbeef"
	)

	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+pathMarker, nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/invoices?filter="+queryMarker, nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"x","password":"`+bodyMarker+`"}`)),
	}
	requests[0].Header.Set("Authorization", authMarker)
	requests[1].Header.Set("Cookie", cookieMark)
	for _, r := range requests {
		r.Header.Set("Content-Type", "application/json")
	}

	for _, r := range requests {
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}

	body := scrapeMetrics(t, m)
	for _, marker := range []string{authMarker, cookieMark, bodyMarker, pathMarker, queryMarker} {
		if strings.Contains(body, marker) {
			t.Fatalf("expected marker %q never to appear in the metrics exposition, got:\n%s", marker, body)
		}
	}

	// A broader structural check: nothing resembling a raw UUID (this
	// app's own path-parameter shape) should ever appear as a label
	// value either.
	if strings.Contains(body, "550e8400") {
		t.Fatal("expected no fragment of a raw path parameter to appear in the exposition")
	}
}

// TestApp_MetricsExposition_RealRequestData_NeverContainsBusinessData
// drives the same property as the test above through the real
// register -> login -> business-data HTTP/service/repository/PostgreSQL
// stack, with a genuinely distinctive password and customer email, then
// scrapes /metrics and asserts neither ever appears. Skipped when
// DATABASE_URL is unavailable (see newTestPool) — the adversarial test
// above already proves the same property without a live database.
func TestApp_MetricsExposition_RealRequestData_NeverContainsBusinessData(t *testing.T) {
	db := newTestPool(t)
	m := metrics.New(db)
	handler := New(db, testLogger, m).Handler()

	const (
		orgName       = "Metrics Privacy Org"
		adminEmail    = "metrics-privacy-admin@example.test"
		adminPassword = "correct horse battery staple metrics marker"
		customerEmail = "metrics-privacy-customer-marker@example.test"
	)

	registerBody := bytes.NewBufferString(`{
		"organisation": {"name": "` + orgName + `"},
		"user": {"name": "Metrics Admin", "email": "` + adminEmail + `", "password": "` + adminPassword + `"}
	}`)
	registerRecorder := doRequest(handler, http.MethodPost, "/api/v1/register", "", registerBody)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d (body: %s)", registerRecorder.Code, registerRecorder.Body.String())
	}
	var registerResponse struct {
		Organisation struct {
			ID string `json:"id"`
		} `json:"organisation"`
	}
	if err := json.NewDecoder(registerRecorder.Body).Decode(&registerResponse); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	t.Cleanup(func() { cleanupOrganisation(db, registerResponse.Organisation.ID) })

	token := loginAs(t, handler, adminEmail, adminPassword)

	customerBody := bytes.NewBufferString(`{"name":"Metrics Privacy Customer","email":"` + customerEmail + `"}`)
	if recorder := doRequest(handler, http.MethodPost, "/api/v1/customers", token, customerBody); recorder.Code != http.StatusCreated {
		t.Fatalf("create customer: status %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	body := scrapeMetrics(t, m)
	for _, marker := range []string{adminPassword, customerEmail, token, adminEmail} {
		if strings.Contains(body, marker) {
			t.Fatalf("expected marker %q never to appear in the metrics exposition, got:\n%s", marker, body)
		}
	}
}

// TestApp_ConcurrentRequests_MetricsInstrumentationIsRaceFree fires many
// concurrent requests of varying methods/routes/statuses through the
// full metrics-instrumented handler chain — meant to be run with
// `go test -race` (see this milestone's own verification section):
// Prometheus's own collectors are already documented as safe for
// concurrent use, and this proves nothing this application adds around
// them (the statusRecorder, RequestLogging's own bookkeeping) breaks
// that.
func TestApp_ConcurrentRequests_MetricsInstrumentationIsRaceFree(t *testing.T) {
	m := metrics.New(nil)
	handler := New(nil, testLogger, m).Handler()

	const goroutines = 50
	const requestsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < requestsPerGoroutine; i++ {
				// /api/v1/customers/{id} matches its registered pattern
				// (route table registration needs no live database, only
				// a request touching the DB itself would — see New's own
				// doc comment) and returns 401 with a nil pool, since
				// RequireAuth rejects it before any repository call.
				paths := []string{"/health", "/metrics", "/api/v1/customers/11111111-1111-1111-1111-111111111111", "/no/such/route"}
				path := paths[(id+i)%len(paths)]
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			}
		}(g)
	}
	wg.Wait()

	// A concurrent scrape, too — promhttp's handler is documented safe
	// for concurrent use against a live registry.
	_ = scrapeMetrics(t, m)
}

// scrapeMetrics renders m's exposition body as a string for substring
// assertions, mirroring the same small helper httpx's and invoice's own
// metrics test files each keep locally.
func scrapeMetrics(t *testing.T, m *metrics.Metrics) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape /metrics: expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	return recorder.Body.String()
}
