package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-invoicing/internal/metrics"
	"go-invoicing/internal/ratelimit"
)

func TestRateLimit_Returns429WithRetryAfterAndSkipsHandler(t *testing.T) {
	m := metrics.New(nil)
	calls := 0
	handler := RateLimit(metrics.RateLimiterLogin, ratelimit.New(time.Minute, 2, 100), ClientAddressKey, m)(
		func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		},
	)

	serve := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodPost, "/", nil))
		return recorder
	}

	for i := 0; i < 2; i++ {
		if recorder := serve(); recorder.Code != http.StatusOK {
			t.Fatalf("request %d within the limit: expected 200, got %d", i+1, recorder.Code)
		}
	}

	limited := serve()
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", limited.Code)
	}
	assertErrorCode(t, limited, CodeRateLimited)
	if got := limited.Header().Get("Retry-After"); got != "60" {
		t.Errorf("expected Retry-After: 60, got %q", got)
	}
	if calls != 2 {
		t.Errorf("expected the handler to run only for the 2 allowed requests, ran %d times", calls)
	}

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), `go_invoicing_rate_limited_requests_total{limiter="login"} 1`) {
		t.Error("expected one rate-limited request to be counted under limiter=\"login\"")
	}
}

func TestClientAddressKey(t *testing.T) {
	key := func(remoteAddr string, headers ...string) string {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = remoteAddr
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		return ClientAddressKey(r)
	}

	if got := key("203.0.113.7:51234"); got != "addr:203.0.113.7" {
		t.Errorf("IPv4: got %q", got)
	}
	if key("203.0.113.7:1") == key("203.0.113.8:1") {
		t.Error("expected distinct IPv4 addresses to get distinct keys")
	}
	if key("[::ffff:203.0.113.7]:1") != key("203.0.113.7:1") {
		t.Error("expected an IPv4-mapped IPv6 address to key as its IPv4 address")
	}

	// IPv6: one /64 is one key; a different /64 is another.
	if key("[2001:db8:1:2::1]:1") != key("[2001:db8:1:2:ffff::9]:1") {
		t.Error("expected addresses within one IPv6 /64 to share a key")
	}
	if key("[2001:db8:1:2::1]:1") == key("[2001:db8:1:3::1]:1") {
		t.Error("expected different IPv6 /64s to get different keys")
	}

	// Client-supplied forwarding headers must never choose the key.
	base := key("203.0.113.7:1")
	if key("203.0.113.7:1", "X-Forwarded-For", "198.51.100.1", "X-Real-IP", "198.51.100.2") != base {
		t.Error("expected X-Forwarded-For/X-Real-IP to be ignored")
	}
}
