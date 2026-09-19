package httpx

import (
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
