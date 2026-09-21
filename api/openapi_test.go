package openapi

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestSpec_IsNonEmpty guards against the embed directive silently picking
// up an empty or missing file — go:embed itself would fail the build if
// openapi.yaml didn't exist, but not if it were accidentally truncated to
// nothing.
func TestSpec_IsNonEmpty(t *testing.T) {
	if len(Spec) == 0 {
		t.Fatal("expected the embedded OpenAPI spec to be non-empty")
	}
}

// TestSpec_ParsesAndValidatesAsOpenAPI31 is Milestone 8 Part 5's genuine
// semantic validation, replacing Part 4's plain "is this YAML"
// yaml.Unmarshal check (which could not have caught most of what this
// one does): it loads the embedded document through kin-openapi (which
// resolves every internal $ref while loading — an unresolvable one fails
// here with a clear "failed to resolve ... in fragment" error, not a
// silent nil), then runs kin-openapi's own OpenAPI-object validation,
// which walks every operation/parameter/schema and fails on structurally
// invalid OpenAPI (e.g. a schema whose composition keywords don't make
// sense, a response with no description, a parameter with no schema).
//
// See loadAndValidateSpec's own comment for why every other contract
// test in this package (and internal/app's route/DTO contract tests)
// calls this same function rather than loading the document again.
func TestSpec_ParsesAndValidatesAsOpenAPI31(t *testing.T) {
	doc := loadAndValidateSpec(t)

	if doc.OpenAPI != "3.1.0" {
		t.Errorf(`expected "openapi: 3.1.0", got %q`, doc.OpenAPI)
	}

	if doc.Paths == nil || doc.Paths.Len() == 0 {
		t.Error("expected a non-empty paths object")
	}

	if len(doc.Components.Schemas) == 0 {
		t.Error("expected a non-empty components.schemas object")
	}
}

// loadAndValidateSpec loads the embedded specification via kin-openapi
// and runs its document-level Validate — the same two steps
// TestSpec_ParsesAndValidatesAsOpenAPI31 exists to prove succeed. Every
// contract test in this repository that needs the parsed document
// (internal/app's route-drift and DTO contract tests included) calls
// this rather than loading a second time, so a genuine spec regression
// is reported once, clearly, by this package's own test, instead of as
// a confusing failure inside an unrelated package's test.
func loadAndValidateSpec(t *testing.T) *openapi3.T {
	t.Helper()

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(Spec)
	if err != nil {
		t.Fatalf("load embedded OpenAPI spec (this also resolves every internal $ref): %v", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("embedded OpenAPI spec failed OpenAPI 3.1 validation: %v", err)
	}

	return doc
}
