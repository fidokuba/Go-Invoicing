package app

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// validateAgainstSchema marshals value (an actual Go DTO instance — a
// request struct a handler decodes into, or a response struct a handler
// writes out) to JSON, decodes that JSON back into a generic value, and
// validates it against the named component schema in the maintained
// OpenAPI spec. This exercises the real struct's real `json:"..."` tags
// on both sides — the same encoding/json machinery net/http actually
// uses — rather than hand-writing a JSON literal that could quietly
// stop matching the Go type it's meant to represent.
//
// EnableJSONSchema2020() is required for OpenAPI 3.1 documents: without
// it, kin-openapi falls back to its older, 3.0-only validator, which
// does not understand this spec's 3.1-only keywords correctly.
func validateAgainstSchema(t *testing.T, doc *openapi3.T, schemaName string, value any) {
	t.Helper()

	schemaRef, ok := doc.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("no component schema named %q in the OpenAPI spec", schemaName)
	}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}

	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal %T's own JSON back into a generic value: %v", value, err)
	}

	if err := schemaRef.Value.VisitJSON(generic, openapi3.EnableJSONSchema2020()); err != nil {
		t.Errorf("%T's JSON does not satisfy OpenAPI schema %q: %v\nJSON was: %s", value, schemaName, err, data)
	}
}

// validateAgainstArraySchema is validateAgainstSchema's sibling for a
// schema object already in hand (e.g. an array's resolved items schema)
// rather than one looked up by component name.
func validateAgainstArraySchema(t *testing.T, schema *openapi3.Schema, value any) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}

	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal %T's own JSON back into a generic value: %v", value, err)
	}

	if err := schema.VisitJSON(generic, openapi3.EnableJSONSchema2020()); err != nil {
		t.Errorf("%T's JSON does not satisfy the schema: %v\nJSON was: %s", value, err, data)
	}
}

// mustNotValidate is the inverse of validateAgainstSchema: it asserts
// that value's JSON is REJECTED by the named schema. This is only ever
// used to prove a contract test would actually catch drift (see
// TestRequestContract_AdditionalPropertiesAreRejected) — a suite of
// tests that always call validateAgainstSchema could otherwise pass
// vacuously forever if the schema/value pairing were ever silently
// broken (e.g. a typo'd schema name that still happens to exist).
func mustNotValidate(t *testing.T, doc *openapi3.T, schemaName string, value any) {
	t.Helper()

	schemaRef, ok := doc.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("no component schema named %q in the OpenAPI spec", schemaName)
	}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}

	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal %T's own JSON back into a generic value: %v", value, err)
	}

	if err := schemaRef.Value.VisitJSON(generic, openapi3.EnableJSONSchema2020()); err == nil {
		t.Errorf("expected %T's JSON to be rejected by schema %q, but it validated\nJSON was: %s", value, schemaName, data)
	}
}
