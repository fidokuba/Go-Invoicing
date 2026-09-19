package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
)

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
					logger.Error(
						"panic recovered in HTTP handler",
						"panic", recovered,
						"method", r.Method,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)

					WriteError(w, http.StatusInternalServerError, CodeInternalError, "internal server error")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
