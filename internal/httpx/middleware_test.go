package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	handler := RequestID(RequestLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := RequestID(RequestLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		handler := RequestID(RequestLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := RequestID(RequestLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := RequestID(RequestLogging(logger)(mux))

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

	handler := RequestID(RequestLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := RequestID(RequestLogging(logger)(Recover(logger)(mux)))

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
