package app

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestSpecExamples_ValidateAgainstTheirOwnSchema is Milestone 8 Part 5
// section 29. kin-openapi's own document Validate() (see loadSpec) does
// not check examples against their schema, so this walks every
// requestBody and response media-type entry in the maintained spec and,
// wherever both an inline `example:` and a `schema:` are present,
// validates the one against the other.
//
// This covers every example the spec declares — including the ones
// section 29 names explicitly (registration, login, customer, product,
// invoice, invoice list, settings, payment, error) — rather than a
// hand-picked subset, since a generic walk over the parsed document is
// no more code than picking out nine special cases would have been.
func TestSpecExamples_ValidateAgainstTheirOwnSchema(t *testing.T) {
	doc := loadSpec(t)

	checked := 0

	checkContent := func(location string, content openapi3.Content) {
		for mediaType, mt := range content {
			if mt.Example == nil || mt.Schema == nil {
				continue
			}
			checked++
			if err := mt.Schema.Value.VisitJSON(mt.Example, openapi3.EnableJSONSchema2020()); err != nil {
				t.Errorf("%s (%s): example does not satisfy its schema: %v\nexample was: %#v", location, mediaType, err, mt.Example)
			}
		}
	}

	for path, pathItem := range doc.Paths.Map() {
		for _, method := range operationMethods(pathItem) {
			methodUpper := strings.ToUpper(method)
			op := pathItem.Operations()[methodUpper]

			if op.RequestBody != nil {
				checkContent(methodUpper+" "+path+" requestBody", op.RequestBody.Value.Content)
			}
			for status, response := range op.Responses.Map() {
				checkContent(methodUpper+" "+path+" "+status+" response", response.Value.Content)
			}
		}
	}

	if checked == 0 {
		t.Fatal("expected at least one example in the spec to check — none were found at all, which likely means this test is broken rather than that the spec has no examples")
	}
	t.Logf("validated %d inline examples against their schemas", checked)
}
