package openapi

import (
	"testing"

	"gopkg.in/yaml.v3"
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

// TestSpec_IsParseableYAML is the lightweight syntax check Milestone 8
// Part 4 calls for — it only proves the maintained document is
// well-formed YAML and declares itself as an OpenAPI 3.1 document with a
// non-empty paths object. It does not validate the document against the
// OpenAPI 3.1 schema itself (no field-shape, $ref-resolution, or
// duplicate-operation-ID checking) — that comprehensive validation is
// explicitly deferred to Milestone 8 Part 5.
func TestSpec_IsParseableYAML(t *testing.T) {
	var doc map[string]any
	if err := yaml.Unmarshal(Spec, &doc); err != nil {
		t.Fatalf("embedded OpenAPI spec is not valid YAML: %v", err)
	}

	version, ok := doc["openapi"].(string)
	if !ok || version != "3.1.0" {
		t.Errorf(`expected top-level "openapi" field to be "3.1.0", got %v`, doc["openapi"])
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Error(`expected a non-empty top-level "paths" object`)
	}
}
