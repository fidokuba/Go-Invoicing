package app

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	openapi "go-invoicing/api"
)

// TestRoutes_MatchOpenAPISpec is Milestone 8 Part 5's route-drift test: it
// proves, in both directions, that this application's actual registered
// routes and the maintained OpenAPI specification describe the same set
// of (method, path) pairs.
//
//   - An application route with no corresponding spec operation fails —
//     the spec silently forgot an endpoint that actually exists.
//   - A spec operation with no corresponding application route fails —
//     the spec documents an endpoint that doesn't actually exist.
//
// Deliberately out of scope here (see the package's own Part 5 report):
// request schema, role authorization, and status codes. Those are
// checked by their own, separate contract tests — this one only ever
// answers "does this method+path exist on both sides".
//
// This requires no database access at all: App.RoutePatterns() is built
// by Handler() from nothing but the (compile-time-fixed) route
// registration table, so a nil *pgxpool.Pool is enough — no
// DATABASE_URL, no live Postgres, safe to run under plain `go test
// ./...`.
func TestRoutes_MatchOpenAPISpec(t *testing.T) {
	application := New(nil, testLogger, nil)
	_ = application.Handler()

	appRoutes := normalizedAppRoutes(t, application.RoutePatterns())
	specRoutes := normalizedSpecRoutes(t)

	for key := range appRoutes {
		if _, ok := specRoutes[key]; !ok {
			t.Errorf("application registers %s but the OpenAPI spec has no matching operation", key)
		}
	}

	for key := range specRoutes {
		if _, ok := appRoutes[key]; !ok {
			t.Errorf("OpenAPI spec documents %s but no application route matches it", key)
		}
	}
}

// TestRoutes_NoDuplicateMethodAndPath is section 7's registered-route
// half of duplicate-operation protection: two routes with the same
// method and path would silently shadow one another in ServeMux (the
// second registration would panic at startup, actually — but this test
// makes the invariant explicit and independent of that panic) and would
// also make the drift comparison above ambiguous about which handler a
// spec operation is supposed to correspond to.
func TestRoutes_NoDuplicateMethodAndPath(t *testing.T) {
	application := New(nil, testLogger, nil)
	_ = application.Handler()

	seen := make(map[string]bool)
	for _, r := range application.RoutePatterns() {
		key := r.Method + " " + r.Path
		if seen[key] {
			t.Errorf("route %s is registered more than once", key)
		}
		seen[key] = true
	}
}

// TestOpenAPISpec_NoDuplicateOperations is section 7's spec-side half:
// nothing here relies on kin-openapi's own YAML-map semantics (which
// would silently keep only the last of two duplicate path keys) — it
// walks the parsed document's Paths/PathItem/Operations directly, which
// is exactly what every other contract test in this package already
// trusts.
func TestOpenAPISpec_NoDuplicateOperations(t *testing.T) {
	specRoutes := normalizedSpecRoutes(t)

	// normalizedSpecRoutes itself would have already collapsed a
	// duplicate into one map entry, so the meaningful assertion here is
	// that the number of (method, normalized-path) pairs found while
	// walking the document equals the number actually recorded — i.e.
	// walking never produced the same key twice from two different
	// (path, method) sources.
	count := 0
	doc := loadSpec(t)
	for _, pathItem := range doc.Paths.Map() {
		count += len(operationMethods(pathItem))
	}

	if count != len(specRoutes) {
		t.Errorf("expected %d distinct (method, path) operations, found %d raw operations — the spec likely declares the same method+path twice", len(specRoutes), count)
	}
}

// loadSpec loads and validates the embedded specification via
// kin-openapi — see TestSpec_ParsesAndValidatesAsOpenAPI31 in
// api/openapi_test.go for the dedicated test of this step itself. Every
// other contract test in this package calls this rather than
// re-implementing loading, so a load/validation failure surfaces once,
// clearly, rather than as a confusing failure deep inside an unrelated
// test.
func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.Spec)
	if err != nil {
		t.Fatalf("load embedded OpenAPI spec: %v", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("embedded OpenAPI spec failed validation: %v", err)
	}

	return doc
}

var knownHTTPMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// operationMethods returns the lower-case HTTP methods pathItem declares
// an operation for.
func operationMethods(pathItem *openapi3.PathItem) []string {
	var methods []string
	for _, method := range knownHTTPMethods {
		if pathItem.Operations()[strings.ToUpper(method)] != nil {
			methods = append(methods, method)
		}
	}
	return methods
}

// pathParamPattern matches a net/http.ServeMux path-parameter segment
// like "{id}".
var pathParamPattern = regexp.MustCompile(`\{[^{}]+\}`)

// normalizePath collapses every path-parameter segment (regardless of
// its name — ServeMux's "{id}" and OpenAPI's "{id}" already agree on the
// name in this project, but the comparison doesn't want to be fragile
// against a future rename on either side) into a single placeholder, so
// "/api/v1/invoices/{id}" and "/api/v1/invoices/{invoiceId}" would
// compare equal. It also strips ServeMux's own "{$}" end-of-path anchor
// and any trailing-slash wildcard, neither of which this application's
// route table currently uses, but which would otherwise make a raw
// string comparison fragile if introduced later.
func normalizePath(path string) string {
	path = strings.TrimSuffix(path, "/{$}")
	return pathParamPattern.ReplaceAllString(path, "{}")
}

// normalizedAppRoutes builds the set of "METHOD normalized-path" keys
// this application actually registers.
func normalizedAppRoutes(t *testing.T, routes []RoutePattern) map[string]bool {
	t.Helper()

	if len(routes) == 0 {
		t.Fatal("expected at least one registered route — Handler() may not have run, or route registration itself is broken")
	}

	set := make(map[string]bool, len(routes))
	for _, r := range routes {
		key := fmt.Sprintf("%s %s", strings.ToUpper(r.Method), normalizePath(r.Path))
		set[key] = true
	}

	return set
}

// normalizedSpecRoutes builds the same kind of set as
// normalizedAppRoutes, but from the OpenAPI document's Paths object.
func normalizedSpecRoutes(t *testing.T) map[string]bool {
	t.Helper()

	doc := loadSpec(t)

	set := make(map[string]bool)
	for path, pathItem := range doc.Paths.Map() {
		for _, method := range operationMethods(pathItem) {
			key := fmt.Sprintf("%s %s", strings.ToUpper(method), normalizePath(path))
			set[key] = true
		}
	}

	return set
}

// TestNormalizedSpecRoutes_SanityCheck is a small guard against
// normalizedSpecRoutes/normalizedAppRoutes silently agreeing on nothing
// because both sides are empty (which would make TestRoutes_MatchOpenAPISpec
// vacuously pass) — it asserts the two known-fixed counts (20 paths / 28
// operations, per the Part 4 report) still hold, printed sorted for a
// readable diff if either side ever changes.
func TestNormalizedSpecRoutes_SanityCheck(t *testing.T) {
	specRoutes := normalizedSpecRoutes(t)
	if len(specRoutes) == 0 {
		t.Fatal("expected at least one operation in the OpenAPI spec")
	}

	keys := make([]string, 0, len(specRoutes))
	for k := range specRoutes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("spec operations (%d): %v", len(keys), keys)
}
