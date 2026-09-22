package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-invoicing/internal/metrics"
)

// TestWrapMethodNotAllowed_ConvertsRouterGenerated405 proves the wrapper
// rewrites a 405 (as net/http.ServeMux itself generates for a
// registered-path-wrong-method request) into the JSON envelope while
// preserving the Allow header.
func TestWrapMethodNotAllowed_ConvertsRouterGenerated405(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /widgets", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the registered handler must not run for a GET request")
	})

	wrapped := WrapMethodNotAllowed(mux)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/widgets", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
	}

	if allow := recorder.Header().Get("Allow"); allow != "POST" {
		t.Errorf("expected Allow: POST to be preserved, got %q", allow)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	assertErrorCode(t, recorder, CodeMethodNotAllowed)
}

// TestWrapMethodNotAllowed_LeavesOtherStatusesAlone proves a successful
// (or any non-405) response passes through completely untouched.
func TestWrapMethodNotAllowed_LeavesOtherStatusesAlone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /widgets", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	wrapped := WrapMethodNotAllowed(mux)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/widgets", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf(`expected {"status":"ok"}, got %v`, body)
	}
}

// TestRecover_CatchesPanicAndWritesGeneric500 proves a panicking handler
// never reaches the client as a dropped connection: Recover catches it
// and writes the standard generic JSON 500, with no stack trace or panic
// value in the response body.
func TestRecover_CatchesPanicAndWritesGeneric500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went unexpectedly wrong")
	})

	wrapped := Recover(logger)(panicking)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panics", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	body := recorder.Body.String()
	if strings.Contains(body, "something went unexpectedly wrong") {
		t.Errorf("expected the panic value not to reach the response body, got %s", body)
	}

	assertErrorCode(t, recorder, CodeInternalError)
}

func TestRecover_DoesNotInterfereWithNormalRequests(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	wrapped := Recover(logger)(ok)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/fine", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestWriteInternalError_LogsUnderlyingErrorOnceAndSkipsDuplicateGeneric5xx(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := RequestID(RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteInternalError(w, r, "customer lookup", io.ErrUnexpectedEOF)
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/customers/123", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
	if got := recorder.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("expected X-Request-ID response header")
	}

	logText := logs.String()
	if !strings.Contains(logText, "customer lookup") {
		t.Fatal("expected diagnostic log to contain the internal operation name")
	}
	if !strings.Contains(logText, "unexpected EOF") {
		t.Fatal("expected diagnostic log to contain the underlying error")
	}
	if !strings.Contains(logText, "request_id=") {
		t.Fatal("expected diagnostic log to include request_id")
	}
	if strings.Count(logText, "http request failed") > 0 {
		t.Fatal("expected no duplicate generic 5xx error event when a detailed internal error has already been logged")
	}
	if strings.Contains(logText, "internal server error") && !strings.Contains(logText, "unexpected EOF") {
		t.Fatal("expected the detailed underlying error to be the diagnostic log, not just the generic client message")
	}
}

func TestRequestLogging_FallbackGeneric5xxLogsOnceAndCompletesOnce(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := RequestID(RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusInternalServerError, CodeInternalError, "internal server error")
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	logText := logs.String()
	if strings.Count(logText, "http request failed") != 1 {
		t.Fatalf("expected exactly one generic 5xx error log, got %d: %s", strings.Count(logText, "http request failed"), logText)
	}
	if strings.Count(logText, "http request completed") != 1 {
		t.Fatalf("expected exactly one completion log, got %d: %s", strings.Count(logText, "http request completed"), logText)
	}
	if !strings.Contains(logText, "request_id=") {
		t.Fatal("expected fallback 5xx log to include request_id")
	}
	if strings.Contains(logText, "internal request error") {
		t.Fatal("expected fallback 5xx path not to emit the detailed internal-error log")
	}
}

func TestRequestLogging_4xxResponsesDoNotGenerateErrorLogs(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict} {
		handler := RequestID(RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, status, "test_error", "example message")
		})))

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))

		if recorder.Code != status {
			t.Fatalf("expected status %d, got %d", status, recorder.Code)
		}
	}

	logText := logs.String()
	if strings.Contains(logText, "http request failed") || strings.Contains(logText, "internal request error") {
		t.Fatal("expected 4xx responses to remain out of ERROR logs, got: " + logText)
	}
	if strings.Count(logText, "http request completed") == 0 {
		t.Fatal("expected 4xx responses to keep the normal completion INFO log")
	}
}

func TestRequestIDMiddleware_GeneratesAndReturnsRequestID(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := RequestID(RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got == "" {
			t.Fatal("expected request ID in context")
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/invoices/550e8400-e29b-41d4-a716-446655440000", nil)
	req.Header.Set("X-Request-ID", "attacker-controlled-value")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if got := recorder.Header().Get("X-Request-ID"); got == "" || got == "attacker-controlled-value" {
		t.Fatalf("expected generated request ID in response header, got %q", got)
	}

	if !strings.Contains(logs.String(), "http request completed") {
		t.Fatal("expected request completion log output")
	}

	if strings.Contains(logs.String(), "attacker-controlled-value") {
		t.Fatal("expected incoming request ID to be ignored in logs")
	}
}

func TestRequestLogging_UsesMatchedRoutePatternAndSuppressesHealthLogs(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RequestID(RequestLogging(logger, nil)(mux))

	for _, tc := range []struct {
		name string
		url  string
	}{
		{name: "invoice", url: "/api/v1/invoices/550e8400-e29b-41d4-a716-446655440000"},
		{name: "health", url: "/health"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}
		})
	}

	if !strings.Contains(logs.String(), "/api/v1/invoices/{id}") {
		t.Fatal("expected matched route pattern in request log output")
	}
	if strings.Contains(logs.String(), "550e8400-e29b-41d4-a716-446655440000") {
		t.Fatal("expected raw path identifier not to appear in request logs")
	}
	if strings.Contains(logs.String(), "level=INFO") && strings.Contains(logs.String(), "http request completed") && strings.Contains(logs.String(), "/health") {
		t.Fatal("expected successful health check request logs to be suppressed from Info level")
	}
}

func TestRequestLogging_RecordsStatusAndSizeAndDoesNotLogSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := RequestID(RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
		if got := RequestIDFromContext(context.Background()); got != "" {
			t.Fatal("context request ID should not be leaked from an unrelated context")
		}
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/organisation", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req.URL.RawQuery = "email=alice@example.com&token=super-secret"
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	logText := logs.String()
	if !strings.Contains(logText, "status=401") && !strings.Contains(logText, "status=\"401\"") {
		t.Fatal("expected request log to include captured status")
	}
	if !strings.Contains(logText, "response_size=") {
		t.Fatal("expected request log to include response size")
	}
	if strings.Contains(logText, "secret-token") || strings.Contains(logText, "super-secret") || strings.Contains(logText, "alice@example.com") {
		t.Fatal("expected request log to avoid logging auth, query, and user-supplied secret values")
	}
}

func TestRecover_LogsRequestIDAndSafeRouteForPanics(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	handler := RequestID(RequestLogging(logger, nil)(Recover(logger)(mux)))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/orders/123", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
	logText := logs.String()
	if !strings.Contains(logText, "panic recovered in HTTP handler") {
		t.Fatal("expected panic log output")
	}
	if !strings.Contains(logText, "request_id=") {
		t.Fatal("expected panic log to include request_id")
	}
	if !strings.Contains(logText, "/api/v1/orders/{id}") {
		t.Fatal("expected panic log to use safe route pattern")
	}
	if strings.Contains(logText, "/api/v1/orders/123") {
		t.Fatal("expected panic log to avoid raw path values")
	}
	if strings.Contains(logText, "http request failed") {
		t.Fatal("expected panic path to avoid a duplicate generic 5xx error event")
	}
}

// --- Milestone 10 Part 4: metrics integration ---

// TestRequestLogging_RecordsHTTPMetricsWithBoundedLabels proves
// RequestLogging feeds ObserveHTTPRequest exactly the method/route/status
// this middleware already computes for logging — never the raw path —
// and that the duration histogram receives an observation too.
func TestRequestLogging_RecordsHTTPMetricsWithBoundedLabels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := metrics.New(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RequestID(RequestLogging(logger, m)(mux))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/invoices/550e8400-e29b-41d4-a716-446655440000", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := scrapeMetrics(t, m)

	if !strings.Contains(body, `go_invoicing_http_requests_total{method="GET",route="/api/v1/invoices/{id}",status="200"} 1`) {
		t.Fatalf("expected a bounded-label counter sample, got:\n%s", body)
	}
	if strings.Contains(body, "550e8400-e29b-41d4-a716-446655440000") {
		t.Fatal("expected the raw UUID path segment never to appear as a metric label")
	}
	if !strings.Contains(body, "go_invoicing_http_request_duration_seconds_bucket") {
		t.Fatal("expected the duration histogram to have received an observation")
	}
}

// TestRequestLogging_UnmatchedRouteUsesBoundedFallbackLabel proves a
// request that never matches any registered pattern is recorded under
// the fixed "unmatched" route label, not the raw request path.
func TestRequestLogging_UnmatchedRouteUsesBoundedFallbackLabel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := metrics.New(nil)

	// A bare handler run directly (as app.go's own final net/http.ServeMux
	// 404 path effectively is): r.Pattern is never populated because
	// nothing routed through a matched net/http.ServeMux pattern.
	handler := RequestID(RequestLogging(logger, m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/no/such/route", nil))

	body := scrapeMetrics(t, m)
	if !strings.Contains(body, `go_invoicing_http_requests_total{method="GET",route="unmatched",status="404"} 1`) {
		t.Fatalf("expected the bounded \"unmatched\" fallback route label, got:\n%s", body)
	}
	if strings.Contains(body, "/no/such/route") {
		t.Fatal("expected the raw unmatched path never to appear as a metric label")
	}
}

// TestRequestLogging_MetricsScrapeItselfIsExcludedFromHTTPMetrics proves
// GET /metrics is never counted in its own http_requests_total series —
// section 13's self-observation exclusion.
func TestRequestLogging_MetricsScrapeItselfIsExcludedFromHTTPMetrics(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := metrics.New(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", m.Handler().ServeHTTP)

	handler := RequestID(RequestLogging(logger, m)(mux))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := scrapeMetrics(t, m)
	if strings.Contains(body, `route="/metrics"`) {
		t.Fatal("expected a /metrics scrape not to appear in http_requests_total at all")
	}
}

// TestRequestLogging_NilMetricsIsANoOp proves passing nil for m (metrics
// disabled) never panics and every existing logging behaviour is
// unaffected — already exercised implicitly by every other test in this
// file passing nil, but asserted explicitly here as its own guarantee.
func TestRequestLogging_NilMetricsIsANoOp(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := RequestID(RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/fine", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

// scrapeMetrics renders m's exposition body as a string, for substring
// assertions — deliberately not a full-output equality assertion (see
// this milestone's own section 38 guidance to avoid over-asserting the
// entire exposition).
func scrapeMetrics(t *testing.T, m *metrics.Metrics) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape /metrics: expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	return recorder.Body.String()
}

// --- Milestone 10 Part 5: adversarial hardening tests ---

// TestRequestLogging_ArbitraryHTTPMethodNormalizedForMetricsCardinality
// proves an attacker-controlled HTTP method token (net/http's server
// accepts any syntactically valid token, matched or not against any
// registered route) can never itself become an unbounded metric label
// value — it must normalize to the fixed "OTHER" fallback. The raw
// method is still fine to keep in the log line itself (see
// normalizeHTTPMethod's own doc comment: a log line is not a
// permanently-retained time-series label), so it is asserted present
// there instead of scrubbed.
func TestRequestLogging_ArbitraryHTTPMethodNormalizedForMetricsCardinality(t *testing.T) {
	const maliciousMethod = "X-ATTACKER-CONTROLLED-METHOD-MARKER"

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := metrics.New(nil)

	handler := RequestID(RequestLogging(logger, m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	request := httptest.NewRequest(maliciousMethod, "/whatever", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	body := scrapeMetrics(t, m)
	if strings.Contains(body, maliciousMethod) {
		t.Fatalf("expected the arbitrary method never to appear as a metric label, got:\n%s", body)
	}
	if !strings.Contains(body, `go_invoicing_http_requests_total{method="OTHER",route="unmatched",status="200"} 1`) {
		t.Fatalf("expected the bounded \"OTHER\" method fallback, got:\n%s", body)
	}

	// The log line, unlike the metric, is not a permanently-retained
	// label set — recording the raw method there is intentional (see
	// normalizeHTTPMethod's own doc comment) and asserted here so a
	// future change doesn't silently start scrubbing it without
	// noticing.
	if !strings.Contains(logs.String(), maliciousMethod) {
		t.Fatal("expected the raw method to still be present in the log line")
	}
}

// TestRequestLogging_KnownMethodsAreNeverNormalizedAway proves every
// method this application actually routes on passes normalizeHTTPMethod
// unchanged — the bounded fallback only ever fires for something outside
// this fixed set.
func TestRequestLogging_KnownMethodsAreNeverNormalizedAway(t *testing.T) {
	m := metrics.New(nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions,
	} {
		handler := RequestID(RequestLogging(logger, m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/whatever", nil))
	}

	body := scrapeMetrics(t, m)
	if strings.Contains(body, `method="OTHER"`) {
		t.Fatalf("expected none of the application's own known methods to fall back to OTHER, got:\n%s", body)
	}
}

// TestWriteInternalError_UnderlyingErrorNeverReachesResponseBody is the
// client-facing half of the exactly-once-failure/error-string audit:
// whatever a handler passes as the underlying error to WriteInternalError
// must stay server-side (in the log) and never leak into the JSON body
// the client actually receives, however sensitive-looking the message
// text is.
func TestWriteInternalError_UnderlyingErrorNeverReachesResponseBody(t *testing.T) {
	const marker = "customer secret-marker@example.test row detail"

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := RequestID(RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteInternalError(w, r, "customer lookup", errors.New(marker))
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/customers/123", nil))

	if strings.Contains(recorder.Body.String(), marker) {
		t.Fatalf("expected the underlying error never to reach the response body, got: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "internal server error") {
		t.Fatalf("expected the generic client-facing message, got: %s", recorder.Body.String())
	}
	if !strings.Contains(logs.String(), marker) {
		t.Fatal("expected the underlying error to still reach the server-side log")
	}
}

// TestRequestLogging_RequestBodyNeverLogged proves a request body
// (however sensitive its content) is never read or logged by the request
// logging/metrics path — nothing in RequestLogging or ObserveHTTPRequest
// ever touches r.Body, and this pins that property down explicitly.
func TestRequestLogging_RequestBodyNeverLogged(t *testing.T) {
	const bodyMarker = "PLAINTEXT-PASSWORD-abc123-DO-NOT-LOG"

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := metrics.New(nil)

	handler := RequestID(RequestLogging(logger, m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // the handler itself reads the body, as a real one would
		w.WriteHeader(http.StatusOK)
	})))

	body := bytes.NewBufferString(`{"password":"` + bodyMarker + `"}`)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body))

	if strings.Contains(logs.String(), bodyMarker) {
		t.Fatal("expected the request body never to appear in logs")
	}
	if strings.Contains(scrapeMetrics(t, m), bodyMarker) {
		t.Fatal("expected the request body never to appear in metrics")
	}
}

// TestRequestLogging_UnmatchedRoute_PathAndQueryMarkersNeverSurface is the
// combined 404-privacy adversarial test: a completely unmatched path
// carrying both a distinctive path-segment marker and a distinctive query
// string marker must produce the bounded "unmatched" route in both logs
// and metrics, with neither marker appearing anywhere in either.
func TestRequestLogging_UnmatchedRoute_PathAndQueryMarkersNeverSurface(t *testing.T) {
	const pathMarker = "ATTACKER-PATH-MARKER-9f8e7d"
	const queryMarker = "ATTACKER-QUERY-MARKER-1a2b3c"

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := metrics.New(nil)

	// No mux at all — mirrors the genuinely-unmatched-route shape
	// (r.Pattern never populated) documented on
	// TestRequestLogging_UnmatchedRouteUsesBoundedFallbackLabel above.
	handler := RequestID(RequestLogging(logger, m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})))

	request := httptest.NewRequest(http.MethodGet, "/does/not/exist/"+pathMarker+"?token="+queryMarker, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	logText := logs.String()
	metricsBody := scrapeMetrics(t, m)

	for _, marker := range []string{pathMarker, queryMarker} {
		if strings.Contains(logText, marker) {
			t.Fatalf("expected marker %q never to appear in logs, got: %s", marker, logText)
		}
		if strings.Contains(metricsBody, marker) {
			t.Fatalf("expected marker %q never to appear in metrics, got:\n%s", marker, metricsBody)
		}
	}

	if !strings.Contains(metricsBody, `go_invoicing_http_requests_total{method="GET",route="unmatched",status="404"} 1`) {
		t.Fatalf("expected the bounded \"unmatched\" route label, got:\n%s", metricsBody)
	}
}
