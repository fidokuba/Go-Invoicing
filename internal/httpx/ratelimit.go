package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

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
// connected to this server (r.RemoteAddr), never a forwarding header —
// ClientAddressKeyFunc(nil). IPv6 addresses are grouped by their /64
// prefix: one subscriber/host typically controls a whole /64, so
// per-address keys would be trivially evaded.
func ClientAddressKey(r *http.Request) string {
	return addressKey(peerAddress(r), r.RemoteAddr)
}

// ClientAddressKeyFunc (Milestone 13 Part 5) returns a key function that
// finds the real client behind the given trusted reverse proxies
// (config.TrustedProxies / TRUSTED_PROXIES).
//
// X-Forwarded-For is consulted only when the connecting peer itself is
// a trusted proxy; from any other peer it is ignored, since any client
// can send it. Even then, only the part the trusted proxies appended can
// be believed: each proxy appends the address it received the request
// from, so the client is the rightmost entry that is not itself a
// trusted proxy. Entries further left were supplied by the client and
// are never used. If the header is missing or malformed, or every entry
// is a trusted proxy, the peer's own address is the key — the safe
// fallback that at worst shares one bucket.
//
// With no trusted proxies this is exactly ClientAddressKey.
func ClientAddressKeyFunc(trusted []netip.Prefix) func(*http.Request) string {
	if len(trusted) == 0 {
		return ClientAddressKey
	}

	isTrusted := func(addr netip.Addr) bool {
		for _, prefix := range trusted {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}

	return func(r *http.Request) string {
		peer := peerAddress(r)
		if !peer.IsValid() || !isTrusted(peer) {
			return addressKey(peer, r.RemoteAddr)
		}

		hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break
			}
			hop = hop.Unmap()
			if !isTrusted(hop) {
				return addressKey(hop, "")
			}
		}

		return addressKey(peer, r.RemoteAddr)
	}
}

// peerAddress parses r.RemoteAddr's host, or returns the zero Addr.
func peerAddress(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}

	return addr.Unmap()
}

// addressKey is addr's limiter key (IPv6 grouped by /64), or raw when
// addr couldn't be parsed.
func addressKey(addr netip.Addr, raw string) string {
	if !addr.IsValid() {
		return "addr:" + raw
	}

	if addr.Is6() {
		prefix, _ := addr.Prefix(64)
		return "addr:" + prefix.String()
	}

	return "addr:" + addr.String()
}
