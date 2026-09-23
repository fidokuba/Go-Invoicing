package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFrontendFallback_InterceptsGenuinelyUnmatchedGET proves the one
// case this middleware exists for: a GET request net/http.ServeMux
// found no registered pattern for at all (r.Pattern == "") gets handed
// to serve instead of ServeMux's own plain-text 404.
func TestFrontendFallback_InterceptsGenuinelyUnmatchedGET(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /widgets", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	served := false
	wrapped := FrontendFallback(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>spa</html>"))
	})(mux)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/invoices/abc-123", nil))

	if !served {
		t.Fatal("expected serve to be called for a genuinely unmatched GET request")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if body := recorder.Body.String(); body != "<html>spa</html>" {
		t.Fatalf("expected serve's own body, got %q", body)
	}
}

// TestFrontendFallback_LeavesRealRoutesAlone proves a request that
// actually matches a registered pattern is never touched, whatever its
// status — including a route's own domain-specific 404.
func TestFrontendFallback_LeavesRealRoutesAlone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /widgets/{id}", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "widget_not_found", "widget not found")
	})

	wrapped := FrontendFallback(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("serve must not be called for a request that matched a real route")
	})(mux)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/widgets/123", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	assertErrorCode(t, recorder, "widget_not_found")
}

// TestFrontendFallback_LeavesNonGETMethodsAlone is the specific
// regression this middleware was designed around: a wrong-method or
// otherwise-unmatched non-GET/HEAD request against a genuinely
// unregistered path must keep getting the exact plain 404 it always
// did — never index.html, and never a 405 either (this isn't even a
// method-mismatch against a real path; nothing matches this path at
// all).
func TestFrontendFallback_LeavesNonGETMethodsAlone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /widgets", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not run for an unrelated path")
	})

	wrapped := FrontendFallback(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("serve must never be called for a non-GET/HEAD request")
	})(mux)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/some/unregistered/path", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); ct == "application/json" {
		t.Errorf("expected the plain net/http default 404, not a JSON envelope, got Content-Type %q", ct)
	}
}

// TestFrontendFallback_LeavesRouterGenerated405Alone proves a genuine
// 405 (same path, different registered method) is untouched — this
// middleware only ever looks at 404s.
func TestFrontendFallback_LeavesRouterGenerated405Alone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /widgets", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not run for a GET request")
	})

	wrapped := FrontendFallback(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("serve must never be called for a 405")
	})(WrapMethodNotAllowed(mux))

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/widgets", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
	}
}

// TestFrontendFallback_PassesThroughSuccessfulWriteWithNoExplicitHeader
// covers a handler that calls Write without ever calling WriteHeader
// itself (implicit 200) — must still pass through untouched.
func TestFrontendFallback_PassesThroughSuccessfulWriteWithNoExplicitHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /widgets", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	wrapped := FrontendFallback(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("serve must not be called")
	})(mux)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/widgets", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if recorder.Body.String() != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", recorder.Body.String())
	}
}

// TestFrontendFallback_InterceptsUnmatchedHEAD proves HEAD is treated
// the same as GET (net/http.ServeMux itself already serves HEAD for any
// GET-registered pattern; this middleware must extend the same
// treatment to the "no pattern matched at all" case).
func TestFrontendFallback_InterceptsUnmatchedHEAD(t *testing.T) {
	mux := http.NewServeMux()

	served := false
	wrapped := FrontendFallback(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	})(mux)

	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/invoices/abc", nil))

	if !served {
		t.Fatal("expected serve to be called for an unmatched HEAD request")
	}
}
