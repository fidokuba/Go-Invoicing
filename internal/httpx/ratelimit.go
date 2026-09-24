package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"

	"go-invoicing/internal/metrics"
	"go-invoicing/internal/ratelimit"
)

// RateLimit wraps next so each request first takes a token from limiter
// under the key returned by keyOf (Milestone 13 Part 4). When the key's
// bucket is empty the request is rejected with 429 rate_limited and a
// Retry-After header (whole seconds) — the only limiter state ever
// exposed — and counted under name, a fixed limiter label. next never
// runs for a rejected request.
func RateLimit(name string, limiter *ratelimit.Limiter, keyOf func(*http.Request) string, m *metrics.Metrics) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			allowed, retryAfter := limiter.Allow(keyOf(r))
			if !allowed {
				m.RecordRateLimited(name)
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				WriteError(w, http.StatusTooManyRequests, CodeRateLimited, "too many requests; please wait before retrying")
				return
			}

			next(w, r)
		}
	}
}

// ClientAddressKey keys a request by the address of the peer that
// connected to this server (r.RemoteAddr). IPv6 addresses are grouped by
// their /64 prefix — one subscriber/host typically controls a whole /64,
// so per-address keys would be trivially evaded.
//
// X-Forwarded-For / X-Real-IP are deliberately ignored: this application
// has no trusted-proxy configuration, and any client can send those
// headers, so honouring them would let a caller pick its own key. The
// consequence for reverse-proxy deployments (see README): every client
// appears as the proxy's address and shares one bucket.
func ClientAddressKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "addr:" + host
	}

	addr = addr.Unmap()
	if addr.Is6() {
		prefix, _ := addr.Prefix(64)
		return "addr:" + prefix.String()
	}

	return "addr:" + addr.String()
}
