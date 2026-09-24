package app

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
)

// Milestone 13 Part 2: the documented optimistic-concurrency contract
// must cover exactly the four operations that implement it, and match
// what the server actually sends.

var concurrencyProtectedPaths = []string{"/api/v1/organisation", "/api/v1/organisation/settings"}

func TestConcurrencyContract_ETagAndIfMatchDocumented(t *testing.T) {
	doc := loadSpec(t)

	for _, path := range concurrencyProtectedPaths {
		get := getOperation(t, doc, path, "GET")
		if _, ok := get.Responses.Status(200).Value.Headers["ETag"]; !ok {
			t.Errorf("GET %s: expected the 200 response to document ETag", path)
		}

		patch := getOperation(t, doc, path, "PATCH")
		if _, ok := patch.Responses.Status(200).Value.Headers["ETag"]; !ok {
			t.Errorf("PATCH %s: expected the 200 response to document ETag", path)
		}
		ifMatch := headerParam(patch, "If-Match")
		if ifMatch == nil || !ifMatch.Required {
			t.Errorf("PATCH %s: expected a required If-Match header parameter", path)
		}
		for _, status := range []int{400, 412, 428} {
			if patch.Responses.Status(status) == nil {
				t.Errorf("PATCH %s: expected a documented %d response", path, status)
			}
		}
	}

	// Nothing else claims optimistic concurrency.
	for path, pathItem := range doc.Paths.Map() {
		for method, op := range pathItem.Operations() {
			if isProtected(path) && (method == "GET" || method == "PATCH") {
				continue
			}
			if headerParam(op, "If-Match") != nil || op.Responses.Status(412) != nil || op.Responses.Status(428) != nil {
				t.Errorf("%s %s unexpectedly documents If-Match/412/428", method, path)
			}
		}
	}
}

// The ETag the server really sends matches the documented format, and
// round-trips as If-Match.
func TestConcurrencyContract_RuntimeETagMatchesSpec(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "OCC Contract Org", "occ-contract-"+uuid.NewString()+"@example.com")
	doc := loadSpec(t)
	pattern := regexp.MustCompile(doc.Components.Headers["VersionETag"].Value.Schema.Value.Pattern)

	for _, path := range concurrencyProtectedPaths {
		etag := etagOf(t, handler, path, tenant.token)
		if !pattern.MatchString(etag) {
			t.Errorf("GET %s: ETag %q doesn't match the documented pattern", path, etag)
		}
		if recorder := doRequestWithIfMatch(handler, path, tenant.token, `{}`, etag); recorder.Code != http.StatusOK || !pattern.MatchString(recorder.Header().Get("ETag")) {
			t.Errorf("PATCH %s: expected 200 with a documented-format ETag, got %d %q", path, recorder.Code, recorder.Header().Get("ETag"))
		}
	}
}

func headerParam(op *openapi3.Operation, name string) *openapi3.Parameter {
	for _, p := range op.Parameters {
		if p.Value.In == "header" && p.Value.Name == name {
			return p.Value
		}
	}
	return nil
}

func isProtected(path string) bool {
	for _, p := range concurrencyProtectedPaths {
		if p == path {
			return true
		}
	}
	return false
}
