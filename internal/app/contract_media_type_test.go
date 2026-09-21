package app

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// jsonBodiedOperations is Milestone 8 Part 5 section 13's fixed,
// explicit list of every operation whose handler actually calls
// httpx.DecodeJSON — confirmed by reading each handler directly (see
// this package's Part 5 report). It is intentionally the same ten
// operations TestRequestContract_MatchesOpenAPISchema covers in
// contract_request_test.go, since those are, by definition, exactly the
// operations that decode a request DTO at all.
var jsonBodiedOperations = map[string]bool{
	"POST /api/v1/auth/login":                  true,
	"POST /api/v1/register":                    true,
	"POST /api/v1/users":                       true,
	"PATCH /api/v1/organisation":               true,
	"PATCH /api/v1/organisation/settings":      true,
	"POST /api/v1/customers":                   true,
	"PUT /api/v1/customers/{}/billing-address": true,
	"POST /api/v1/products":                    true,
	"POST /api/v1/invoices":                    true,
	"POST /api/v1/invoices/{}/payments":        true,
}

// TestRequestBodyContract_OnlyJSONBodiedOperationsDeclareARequestBody is
// section 13: every operation in jsonBodiedOperations must declare a
// required requestBody whose content is exactly application/json; every
// other operation must declare no requestBody at all — including
// bodyless mutating operations like logout and send, which a careless
// copy-paste from a sibling POST operation could otherwise leave with a
// stray, incorrect requestBody.
func TestRequestBodyContract_OnlyJSONBodiedOperationsDeclareARequestBody(t *testing.T) {
	doc := loadSpec(t)

	for path, pathItem := range doc.Paths.Map() {
		for _, method := range operationMethods(pathItem) {
			methodUpper := strings.ToUpper(method)
			op := pathItem.Operations()[methodUpper]
			key := methodUpper + " " + normalizePath(path)

			wantBody := jsonBodiedOperations[key]

			if wantBody {
				if op.RequestBody == nil {
					t.Errorf("%s is expected to be JSON-bodied but declares no requestBody", key)
					continue
				}
				if !op.RequestBody.Value.Required {
					t.Errorf("%s's requestBody should be required (an empty body is always rejected)", key)
				}
				content := op.RequestBody.Value.Content
				if len(content) != 1 || content.Get("application/json") == nil {
					t.Errorf("%s's requestBody should declare exactly application/json, got %v", key, contentTypes(content))
				}
			} else if op.RequestBody != nil {
				t.Errorf("%s declares a requestBody but is not a JSON-bodied operation", key)
			}
		}
	}

	// Sanity check: every entry in jsonBodiedOperations corresponds to an
	// operation that actually exists in the spec, so a stale/typo'd entry
	// (e.g. after a route is renamed) can't silently stop being checked.
	specRoutes := normalizedSpecRoutes(t)
	for key := range jsonBodiedOperations {
		if !specRoutes[key] {
			t.Errorf("jsonBodiedOperations lists %q but no such operation exists in the spec", key)
		}
	}
}

// TestContentTypeContract_ResponseMediaTypes is section 12: a spot check
// of every distinct response media type this API actually produces.
func TestContentTypeContract_ResponseMediaTypes(t *testing.T) {
	doc := loadSpec(t)

	cases := []struct {
		name      string
		path      string
		method    string
		status    string
		mediaType string
	}{
		{"login (JSON)", "/api/v1/auth/login", "POST", "200", "application/json"},
		{"invoice list (JSON)", "/api/v1/invoices", "GET", "200", "application/json"},
		{"invoice PDF", "/api/v1/invoices/{id}/pdf", "GET", "200", "application/pdf"},
		{"OpenAPI spec (YAML)", "/api/v1/openapi.yaml", "GET", "200", "application/yaml"},
		{"health (JSON)", "/health", "GET", "200", "application/json"},
		{"health/db (JSON)", "/health/db", "GET", "200", "application/json"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op := getOperation(t, doc, c.path, c.method)
			response := op.Responses.Value(c.status)
			if response == nil {
				t.Fatalf("no %s response declared for %s %s", c.status, c.method, c.path)
			}
			if response.Value.Content.Get(c.mediaType) == nil {
				t.Errorf("expected %s %s's %s response to declare %s, got %v", c.method, c.path, c.status, c.mediaType, contentTypes(response.Value.Content))
			}
		})
	}
}

// TestContentTypeContract_LogoutHas204NoContent is part of section 12:
// logout's 204 must declare no content at all, matching the real
// handler (see AuthHandler.Logout: w.WriteHeader(http.StatusNoContent),
// no body ever written).
func TestContentTypeContract_LogoutHas204NoContent(t *testing.T) {
	doc := loadSpec(t)

	op := getOperation(t, doc, "/api/v1/auth/logout", "POST")
	response := op.Responses.Value("204")
	if response == nil {
		t.Fatal("no 204 response declared for POST /api/v1/auth/logout")
	}
	if len(response.Value.Content) != 0 {
		t.Errorf("expected the 204 response to declare no content, got %v", contentTypes(response.Value.Content))
	}
}

// idPathOperations is section 14's set of every operation whose path
// contains a `{id}` segment.
func TestPathParameterContract_IdParamsAreRequiredUUIDStrings(t *testing.T) {
	doc := loadSpec(t)

	found := 0
	for path, pathItem := range doc.Paths.Map() {
		if !strings.Contains(path, "{id}") {
			continue
		}

		for _, method := range operationMethods(pathItem) {
			found++
			op := pathItem.Operations()[strings.ToUpper(method)]
			param := findParam(t, op, "id")

			if !param.Required {
				t.Errorf("%s %s: {id} parameter should be required", strings.ToUpper(method), path)
			}
			if !param.Schema.Value.Type.Is("string") {
				t.Errorf("%s %s: {id} parameter should be type string, got %v", strings.ToUpper(method), path, param.Schema.Value.Type)
			}
			if param.Schema.Value.Format != "uuid" {
				t.Errorf("%s %s: {id} parameter should be format uuid, got %q", strings.ToUpper(method), path, param.Schema.Value.Format)
			}
		}
	}

	if found == 0 {
		t.Fatal("expected at least one operation with an {id} path parameter")
	}

	// Manual audit (not automatable without a static-analysis pass over
	// every handler): every {id} route's handler
	// (UserHandler.GetByID, CustomerHandler.GetByID/GetBillingAddress/
	// UpsertBillingAddress, ProductHandler.GetByID,
	// InvoiceHandler.GetByID/Send/CreatePayment/GetPayments/GetPDF) calls
	// uuid.Parse(r.PathValue("id")) and returns 400 invalid_request on a
	// parse failure — confirmed by reading each handler directly during
	// this milestone's investigation. No {id} route in this application
	// treats the path parameter as anything other than a UUID, so there
	// is no documented exception to record here.
}

func contentTypes(content openapi3.Content) []string {
	names := make([]string, 0, len(content))
	for k := range content {
		names = append(names, k)
	}
	return names
}
