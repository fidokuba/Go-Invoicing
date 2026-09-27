package template

import (
	"net/http"
	"strconv"
	"strings"

	"go-invoicing/internal/httpx"
)

// Optimistic concurrency over HTTP for PATCH /api/v1/templates/{id},
// mirroring internal/administration/etag.go's own pattern and reasoning
// exactly (a resource's integer version is exposed only as a strong
// ETag and must be sent back as If-Match) — re-implemented here rather
// than imported because that file's own doc comment says it is
// "deliberately local to this package," the same convention this
// package follows for dbExecutor and TxBeginner.

const (
	codePreconditionRequired = "precondition_required"
	codePreconditionFailed   = "precondition_failed"
)

const staleWriteMessage = "this resource has been modified since it was read; fetch the latest version and retry"

func setVersionETag(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", `"`+strconv.FormatInt(version, 10)+`"`)
}

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
