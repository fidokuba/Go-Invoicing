package admin

import (
	"net/http"
	"strconv"
	"strings"

	"go-invoicing/internal/httpx"
)

// Optimistic concurrency over HTTP (Milestone 13 Part 2), for the two
// resources that use it: GET/PATCH /organisation and GET/PATCH
// /organisation/settings. A resource's integer version is exposed only as
// a strong ETag ("<version>") and must be sent back as If-Match on PATCH;
// it never appears in any JSON body. Deliberately local to this package —
// nothing else uses it.

// Error codes for the two precondition failures.
const (
	codePreconditionRequired = "precondition_required"
	codePreconditionFailed   = "precondition_failed"
)

const staleWriteMessage = "this resource has been modified since it was read; fetch the latest version and retry"

// setVersionETag sets the strong ETag representing version.
func setVersionETag(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", `"`+strconv.FormatInt(version, 10)+`"`)
}

// readIfMatchVersion extracts the expected version from a PATCH's
// If-Match header. It writes the error response and returns false when:
//
//   - If-Match is missing or empty: 428 precondition_required;
//   - it is anything other than exactly one strong ETag this API issued —
//     a weak tag (W/"3"), the wildcard (*), a list or repeated header, or
//     a malformed value: 400 invalid_request. A wildcard is rejected
//     rather than honoured because "match any version" would defeat the
//     lost-update protection the header exists for.
func readIfMatchVersion(w http.ResponseWriter, r *http.Request) (int64, bool) {
	values := r.Header.Values("If-Match")

	if len(values) == 0 || (len(values) == 1 && strings.TrimSpace(values[0]) == "") {
		httpx.WriteError(w, http.StatusPreconditionRequired, codePreconditionRequired,
			"If-Match header is required: send the ETag from your last GET of this resource")
		return 0, false
	}

	version, ok := parseVersionETag(values)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest,
			`If-Match must be exactly one strong ETag previously returned by this API, e.g. "3"`)
		return 0, false
	}

	return version, true
}

// parseVersionETag accepts exactly one header value of the form "<n>",
// where n is a positive decimal integer.
func parseVersionETag(values []string) (int64, bool) {
	if len(values) != 1 {
		return 0, false
	}

	tag := strings.TrimSpace(values[0])
	if len(tag) < 3 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return 0, false
	}

	digits := tag[1 : len(tag)-1]
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}

	version, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || version < 1 {
		return 0, false
	}

	return version, true
}
