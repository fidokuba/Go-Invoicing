package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// MaxRequestBodyBytes bounds every JSON request body this API accepts.
// 2 MiB comfortably fits an invoice with a large number of lines (each
// line is a small, fixed set of scalar fields) while still being far too
// small for an adversarial client to use as a memory-exhaustion vector.
// One constant is used everywhere rather than a per-endpoint value,
// since no current endpoint's legitimate payload comes anywhere close to
// it.
const MaxRequestBodyBytes = 2 << 20 // 2 MiB

// jsonMediaType is the only request media type this API's JSON endpoints
// accept, compared using mime.ParseMediaType so a parameter such as
// "; charset=utf-8" doesn't cause a false rejection.
const jsonMediaType = "application/json"

// DecodeJSON decodes r's body into dst and reports whether it succeeded.
// On failure it has already written a complete, safe error response
// (415 for a missing/wrong Content-Type, 413 for an oversized body, 400
// for anything else) — the caller only needs to return.
//
// Required behaviour, all enforced here rather than left to
// encoding/json's defaults:
//   - Content-Type must be application/json (parameters such as a
//     charset are ignored); missing or any other media type is rejected
//     with 415, never silently decoded anyway.
//   - the body is wrapped in http.MaxBytesReader, so a body larger than
//     MaxRequestBodyBytes fails cleanly instead of being read in full.
//   - DisallowUnknownFields rejects a field the destination type has no
//     matching tag for, instead of silently ignoring it.
//   - after the first value decodes successfully, a second decode call
//     checks for trailing data — DisallowUnknownFields says nothing
//     about additional top-level JSON values following the first one,
//     so that has to be checked explicitly, into a json.RawMessage
//     (which accepts any well-formed value without field-checking) so
//     this check only ever reports "is there more input", never a
//     second, unrelated decode error.
//   - every failure mode collapses to one of a handful of fixed, safe
//     messages; encoding/json's own error text (which can include raw
//     field/type names) is never sent to the client.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !requireJSONContentType(w, r) {
		return false
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		writeDecodeError(w, err)
		return false
	}

	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, CodeInvalidRequest, "request body must contain a single JSON value")
		return false
	}

	return true
}

// requireJSONContentType reports whether r declares an application/json
// media type, ignoring any parameters (e.g. "; charset=utf-8"). A
// missing header parses to an error here (mime.ParseMediaType rejects an
// empty string), which is deliberately treated the same as any other
// unsupported media type — a JSON-bodied endpoint requires an explicit
// Content-Type, never a silent assumption.
func requireJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != jsonMediaType {
		WriteError(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}

	return true
}

// writeDecodeError maps a decode failure onto one safe, fixed message —
// never the underlying encoding/json (or http.MaxBytesReader) error
// text, which can contain the destination struct's field/type names or
// other internal detail.
func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		WriteError(w, http.StatusRequestEntityTooLarge, CodeRequestTooLarge, "request body exceeds the maximum allowed size")
		return
	}

	if errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, CodeInvalidRequest, "request body must not be empty")
		return
	}

	// encoding/json returns this as a plain, untyped error — there is no
	// exported sentinel or error type for "unknown field" to match with
	// errors.As/Is, so detecting it means matching the one stable prefix
	// the standard library has used for this message for many Go
	// releases. The raw message is only ever inspected here, server-side
	// — the client always receives the fixed string below, never this
	// one.
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		WriteError(w, http.StatusBadRequest, CodeInvalidRequest, "request body contains an unknown field")
		return
	}

	// Malformed JSON syntax, a value of the wrong type for its field,
	// and anything else encoding/json can return are all collapsed into
	// one generic, safe message.
	WriteError(w, http.StatusBadRequest, CodeInvalidRequest, "request body is not valid JSON")
}
