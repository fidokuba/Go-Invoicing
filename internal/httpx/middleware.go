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

func requestRoute(r *http.Request) string {
	if r == nil {
		return "unmatched"
	}

	if r.Pattern != "" {
		return r.Pattern
	}

	path := strings.TrimSpace(r.URL.Path)
	if path == "" {
		return "unmatched"
	}

	return "unmatched"
}

func isHealthRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	return r.URL != nil && (r.URL.Path == "/health" || r.URL.Path == "/health/db")
}

type statusRecorder struct {
	http.ResponseWriter
	status     int
	size       int
	wroteHeader bool
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

func RequestLogging(logger *slog.Logger) func(http.Handler) http.Handler {
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
			durationMS := time.Since(start).Milliseconds()

			if isHealthRequest(r) && status < http.StatusBadRequest {
				return
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
