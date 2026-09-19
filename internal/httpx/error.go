// Package httpx holds small, shared HTTP mechanics — JSON decoding,
// JSON writing, and the error envelope — used by every handler package
// in this project. It standardizes HTTP plumbing only: it has no
// opinion on what any particular domain/service error means, no generic
// handler type, and no controller abstraction. Each handler still owns
// its own errors.Is chain and decides its own status code and error
// code, exactly as before; this package only gives it a consistent way
// to write the result.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Stable, generic error codes shared across every handler. A handler is
// free to use a more specific code (e.g. "invoice_already_sent",
// "email_already_exists") directly as a string literal where that gives
// a client genuine value — these constants are a convenience for the
// common cases, not an exhaustive registry every error must fit into.
const (
	CodeInvalidRequest       = "invalid_request"
	CodeValidationFailed     = "validation_failed"
	CodeUnauthorized         = "unauthorized"
	CodeForbidden            = "forbidden"
	CodeNotFound             = "not_found"
	CodeConflict             = "conflict"
	CodeInternalError        = "internal_error"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodeRequestTooLarge      = "request_too_large"
	CodeMethodNotAllowed     = "method_not_allowed"
)

// ErrorBody is the one JSON shape every application-generated error
// response in this API uses.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries a stable, machine-readable Code plus a
// human-readable Message. Message must never contain SQL, pgx,
// encoding/json-internal, crypto, filesystem, or PDF-library detail —
// callers pass only fixed, safe strings, exactly as the existing
// http.Error(w, "...", status) call sites already did before this
// package existed.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes value as a JSON body with the given status and
// Content-Type: application/json. Encoding failures are not surfaced to
// the client — by the time Encode can fail, the status line and headers
// are already written, so there is nothing safe left to send instead.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// WriteError writes the standard error envelope. code is a stable,
// machine-readable string (see the Code* constants above); message is a
// safe, human-readable string the handler already knows is fine to show
// a client — WriteError performs no filtering of its own, so callers
// remain responsible for never passing err.Error() through unchecked.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}
