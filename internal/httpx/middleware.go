package httpx

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"go-invoicing/internal/metrics"
)

const requestIDHeader = "X-Request-ID"

type requestIDContextKey struct{}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

func generateRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "request-id-unavailable"
	}

	return hex.EncodeToString(buf)
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := generateRequestID()
		w.Header().Set(requestIDHeader, requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID))
		next.ServeHTTP(w, r)
	})
}

// requestRoute returns the bounded route-pattern label used by both
// request logs and HTTP metrics: the matched net/http.ServeMux pattern's
// path portion only (e.g. "/api/v1/invoices/{id}"), or the fixed
// "unmatched" fallback for anything ServeMux didn't route to a
// registered handler at all (a genuinely unregistered path, or the
// synthetic 404/405 paths ServeMux itself generates) — never the raw
// request path or any path-parameter value.
//
// Milestone 10 Part 5 section 5: every pattern this application ever
// registers (see app.go's register() helper, and its two direct
// mux.Handle/HandleFunc calls for /metrics and /api/v1/openapi.yaml) is
// written as "METHOD /path" — net/http.ServeMux's own pattern syntax —
// so r.Pattern arrives as e.g. "GET /api/v1/invoices/{id}", with the
// method baked into the very same string RequestLogging separately
// records as the "method" field/label. Left as-is, "route" and "method"
// would silently duplicate the same information — routePathFromPattern
// strips that leading method token so route stays a pure path pattern
// and method stays its own independent, already-bounded dimension.
func requestRoute(r *http.Request) string {
	if r == nil {
		return "unmatched"
	}

	if r.Pattern != "" {
		return routePathFromPattern(r.Pattern)
	}

	return "unmatched"
}

// routePathFromPattern strips a net/http.ServeMux pattern's leading
// "METHOD " token, leaving only the path portion. Every pattern this
// application registers has one (see requestRoute's own doc comment), so
// the split is unconditional; a pattern with no space is returned
// verbatim as a safe fallback rather than panicking or guessing.
func routePathFromPattern(pattern string) string {
	if idx := strings.IndexByte(pattern, ' '); idx != -1 {
		return pattern[idx+1:]
	}

	return pattern
}

// httpMethodOther is the bounded fallback label normalizeHTTPMethod
// returns for anything outside the small set of methods this application
// actually registers routes for. HTTP method tokens are
// attacker-controlled (an arbitrary request line can carry any
// syntactically valid token, e.g. "SOMETHINGRANDOM /health") and, left
// unnormalized, would let a single client generate unlimited distinct
// Prometheus label values for go_invoicing_http_requests_total /
// go_invoicing_http_request_duration_seconds — a real cardinality-growth
// vector, not merely a style concern. Logs still record r.Method
// verbatim (see RequestLogging below): a log line is one bounded-size
// text record, not a permanently-retained time-series label, so the raw
// value there stays useful for forensics without the same growth risk.
const httpMethodOther = "OTHER"

// normalizeHTTPMethod maps method onto the small, fixed set of HTTP
// methods this application ever registers a route for, plus "OTHER" for
// anything else (including a garbage/attacker-supplied token) — see
// httpMethodOther's own doc comment for why this exists at all. Only
// used for the metrics label; RequestLogging's log fields keep the raw
// r.Method.
func normalizeHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
		return method
	default:
		return httpMethodOther
	}
}

func isHealthRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	return r.URL != nil && (r.URL.Path == "/health" || r.URL.Path == "/health/db")
}

// metricsScrapePath is GET /metrics itself (see app.go's own comment on
// why it is mounted outside the versioned route table). It is excluded
// from ObserveHTTPRequest below (Milestone 10 Part 4 section 13: a scrape
// of the metrics endpoint is not a business HTTP request, and counting it
// as one would create pointless, ever-growing self-referential traffic
// every time something scrapes this exact endpoint) and, like /health,
// kept out of the ordinary per-request INFO log on success.
const metricsScrapePath = "/metrics"

func isMetricsRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	return r.URL != nil && r.URL.Path == metricsScrapePath
}

type internalErrorRecord struct {
	operation string
	err       error
}

type statusRecorder struct {
	http.ResponseWriter
	status           int
	size             int
	wroteHeader      bool
	internalErr      *internalErrorRecord
	diagnosticLogged bool
}

func (r *statusRecorder) recordInternalError(operation string, err error) {
	if err == nil || r.internalErr != nil {
		return
	}

	r.internalErr = &internalErrorRecord{operation: operation, err: err}
}

func (r *statusRecorder) recordDiagnosticLogged() {
	r.diagnosticLogged = true
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := r.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("response writer does not support hijacking")
}

func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := r.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// RequestLogging returns the request-logging middleware. m records the
// bounded HTTP request-count/duration metrics (Milestone 10 Part 4) from
// exactly the same status/route/duration this middleware already computes
// for logging — a second ResponseWriter wrapper solely for metrics would
// just duplicate statusRecorder above, so this reuses it instead. m may
// be nil (metrics disabled — see config.Config.MetricsEnabled): every
// *metrics.Metrics method is a nil-safe no-op, so no "if enabled" branch
// is needed here.
func RequestLogging(logger *slog.Logger, m *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)

			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			requestID := RequestIDFromContext(r.Context())
			route := requestRoute(r)
			duration := time.Since(start)
			durationMS := duration.Milliseconds()

			// A /metrics scrape is excluded from the business HTTP
			// metrics entirely (see metricsScrapePath's own comment) but
			// every other request — /health and /health/db included, on
			// the theory that request-rate/latency/status data for the
			// two liveness/readiness probes is itself useful availability
			// signal, not noise — is recorded regardless of status.
			if !isMetricsRequest(r) {
				m.ObserveHTTPRequest(normalizeHTTPMethod(r.Method), route, status, duration)
			}

			quiet := isHealthRequest(r) || isMetricsRequest(r)
			if quiet && status < http.StatusBadRequest {
				return
			}

			if status >= http.StatusInternalServerError && !quiet {
				if recorder.internalErr != nil {
					logger.Error(
						"internal request error",
						"request_id", requestID,
						"method", r.Method,
						"route", route,
						"status", status,
						"duration_ms", durationMS,
						"response_size", recorder.size,
						"operation", recorder.internalErr.operation,
						"error", recorder.internalErr.err,
					)
					recorder.recordDiagnosticLogged()
				} else if !recorder.diagnosticLogged {
					logger.Error(
						"http request failed",
						"request_id", requestID,
						"method", r.Method,
						"route", route,
						"status", status,
						"duration_ms", durationMS,
						"response_size", recorder.size,
					)
					recorder.recordDiagnosticLogged()
				}
			}

			logger.Info(
				"http request completed",
				"request_id", requestID,
				"method", r.Method,
				"route", route,
				"status", status,
				"duration_ms", durationMS,
				"response_size", recorder.size,
			)
		})
	}
}

// methodNotAllowedInterceptor is a thin http.ResponseWriter decorator
// that rewrites a 405 response's body/Content-Type into the standard
// JSON error envelope while preserving whatever Allow header the
// wrapped writer already set. It leaves every other status code
// completely alone.
type methodNotAllowedInterceptor struct {
	http.ResponseWriter
	intercepting bool
}

func (i *methodNotAllowedInterceptor) WriteHeader(status int) {
	if status != http.StatusMethodNotAllowed {
		i.ResponseWriter.WriteHeader(status)
		return
	}

	// The Allow header (if any) was already set on the underlying
	// ResponseWriter by whatever called WriteHeader — Header() below
	// reaches straight through the embedded interface, so it's
	// preserved automatically; only Content-Type needs overriding here.
	i.intercepting = true
	i.ResponseWriter.Header().Set("Content-Type", "application/json")
	i.ResponseWriter.WriteHeader(status)
	_ = json.NewEncoder(i.ResponseWriter).Encode(ErrorBody{
		Error: ErrorDetail{Code: CodeMethodNotAllowed, Message: "method not allowed"},
	})
}

func (i *methodNotAllowedInterceptor) Write(b []byte) (int, error) {
	if i.intercepting {
		// Discard whatever body the router (or anything else that wrote
		// a 405) was about to send — the JSON envelope was already
		// written by WriteHeader above.
		return len(b), nil
	}

	return i.ResponseWriter.Write(b)
}

// WrapMethodNotAllowed converts net/http.ServeMux's own internally
// generated 405 Method Not Allowed response (issued when a path matches
// a registered pattern but the request's method doesn't — this never
// invokes any registered handler at all, so there is no application
// code to change) into the standard JSON error envelope, without
// replacing ServeMux or duplicating its routing table.
//
// This is safe to apply unconditionally because nothing else in this
// application ever writes a 405 itself (the old inline /health and
// /health/db method checks were converted to use the shared JSON writer
// too — see app.go) — so every 405 this middleware observes is
// genuinely router-generated, never an application handler's own
// decision that this middleware would otherwise be second-guessing.
//
// This does not attempt to distinguish or rewrite 404s: many handlers
// legitimately return their own domain 404 (customer not found, invoice
// not found, ...), and a bare status code gives no way to tell that
// apart from an unmatched route. A dedicated catch-all ("/") pattern was
// considered for the unmatched-route case specifically, but rejected —
// registering one changes ServeMux's own routing precedence enough that
// it silently swallows the automatic 405 this middleware exists to
// convert (see app.go's own comment for the empirical detail) — so an
// unmatched route is deliberately left to net/http's plain-text default,
// per this milestone's own guidance to leave router semantics alone
// rather than fight them.
func WrapMethodNotAllowed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&methodNotAllowedInterceptor{ResponseWriter: w}, r)
	})
}

// FrontendFallback (Milestone 12) intercepts net/http.ServeMux's own
// default, pattern-less 404 for a GET or HEAD request only, and hands it
// to serve instead of net/http's stdlib body — this is how the embedded
// frontend (internal/webui) renders its SPA shell for a client-side
// route, or a real static asset, without this application ever
// registering a route/pattern for the frontend on the mux itself.
//
// That last point is deliberate, not an implementation detail: an
// earlier version of this feature registered a "GET /" pattern directly,
// and it was reverted after proving (via this project's own pre-existing
// test suite) that doing so corrupts net/http.ServeMux's automatic
// 405-vs-404 distinction for *every other unmatched path in the entire
// application* — once any pattern rooted at "/" exists, ServeMux treats
// it as a path match for every otherwise-unregistered path, silently
// turning a wrong-method request against a genuinely nonexistent route
// (e.g. POST to a long-removed unversioned bootstrap path — see
// internal/app's TestApp_UnversionedRoutes_NoLongerWork) from a plain
// 404 into a 405.
//
// This middleware avoids that entirely by never touching the mux's
// routing table. It relies on r.Pattern (Go 1.22+'s ServeMux sets this
// on the request itself, before invoking whatever it decided to route
// to — including its own internal NotFoundHandler, which leaves it
// empty; see requestRoute's own doc comment for the same signal used
// elsewhere) to distinguish "no registered pattern matched this path at
// all" from every other case:
//
//   - r.Pattern != "" (a real route matched, whatever its status) →
//     passed through completely unmodified;
//   - status != 404 (including ServeMux's own synthetic 405, which
//     WrapMethodNotAllowed already owns) → passed through unmodified;
//   - method is neither GET nor HEAD → passed through unmodified, so a
//     POST/PUT/DELETE/etc. against a genuinely unmatched path keeps
//     getting the exact same plain 404 it always did;
//   - otherwise (r.Pattern == "", status 404, GET/HEAD) → serve is
//     called instead, and whatever net/http's own NotFoundHandler was
//     about to write is discarded.
func FrontendFallback(serve http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&frontendFallbackInterceptor{ResponseWriter: w, request: r, serve: serve}, r)
		})
	}
}

type frontendFallbackInterceptor struct {
	http.ResponseWriter
	request      *http.Request
	serve        http.HandlerFunc
	handled      bool
	intercepting bool
}

func (i *frontendFallbackInterceptor) WriteHeader(status int) {
	if i.handled {
		return
	}
	i.handled = true

	if status != http.StatusNotFound || i.request.Pattern != "" || !isGetOrHead(i.request.Method) {
		i.ResponseWriter.WriteHeader(status)
		return
	}

	i.intercepting = true
	i.serve(i.ResponseWriter, i.request)
}

func (i *frontendFallbackInterceptor) Write(b []byte) (int, error) {
	if !i.handled {
		// Writing without an explicit prior WriteHeader implicitly means
		// 200 OK (matching statusRecorder's own equivalent handling
		// above) — go through the same decision path either way.
		i.WriteHeader(http.StatusOK)
	}
	if i.intercepting {
		// Discard whatever net/http's own NotFoundHandler was about to
		// send — Serve already wrote its own response directly to the
		// real ResponseWriter above.
		return len(b), nil
	}

	return i.ResponseWriter.Write(b)
}

func isGetOrHead(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// Recover wraps next so a panic inside any handler is caught, logged
// (message and stack trace, server-side only), and turned into the
// standard generic 500 JSON response instead of the stdlib default
// (dropping the connection with no response body at all). If the
// panicking handler had already written a status/body before panicking
// (e.g. partway through streaming a large PDF), the write below is a
// no-op from the client's perspective — headers already sent cannot be
// un-sent — which is an inherent limit of any recovery middleware, not
// something specific to this implementation.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					requestID := RequestIDFromContext(r.Context())
					logger.Error(
						"panic recovered in HTTP handler",
						"request_id", requestID,
						"method", r.Method,
						"route", requestRoute(r),
						"panic", recovered,
						"stack", string(debug.Stack()),
					)

					if recorder, ok := w.(interface{ recordDiagnosticLogged() }); ok {
						recorder.recordDiagnosticLogged()
					}
					if requestID != "" {
						w.Header().Set(requestIDHeader, requestID)
					}
					WriteError(w, http.StatusInternalServerError, CodeInternalError, "internal server error")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
