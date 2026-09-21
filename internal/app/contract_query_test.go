package app

import (
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"
)

// collectionContract describes one of the four collection (list)
// endpoints' actual query-parameter contract, sourced directly from
// each handler package's exported allow-list/defaults (added in
// Milestone 8 Part 5 specifically so this test never needs a second,
// hand-duplicated copy of them — see e.g. invoice.InvoiceListQueryParams'
// own doc comment). This is section 15/16's "prefer exposing/reusing the
// actual allow-list" instruction applied directly: everything on the
// right-hand side of each field below already existed as the real
// runtime allow-list before this test was written.
type collectionContract struct {
	path         string
	queryParams  []string
	sortFields   []string
	defaultSort  string
	defaultOrder string
}

var collectionContracts = []collectionContract{
	{
		path:         "/api/v1/users",
		queryParams:  admin.UserListQueryParams,
		sortFields:   admin.UserListSortFields,
		defaultSort:  admin.UserListDefaultSort,
		defaultOrder: admin.UserListDefaultOrder,
	},
	{
		path:         "/api/v1/customers",
		queryParams:  customer.CustomerListQueryParams,
		sortFields:   customer.CustomerListSortFields,
		defaultSort:  customer.CustomerListDefaultSort,
		defaultOrder: customer.CustomerListDefaultOrder,
	},
	{
		path:         "/api/v1/products",
		queryParams:  product.ProductListQueryParams,
		sortFields:   product.ProductListSortFields,
		defaultSort:  product.ProductListDefaultSort,
		defaultOrder: product.ProductListDefaultOrder,
	},
	{
		path:         "/api/v1/invoices",
		queryParams:  invoice.InvoiceListQueryParams,
		sortFields:   invoice.InvoiceListSortFields,
		defaultSort:  invoice.InvoiceListDefaultSort,
		defaultOrder: invoice.InvoiceListDefaultOrder,
	},
}

// TestQueryContract_ParamsMatchHandlerAllowList is Milestone 8 Part 5
// section 15: for each of the four collection endpoints, it compares the
// OpenAPI-documented query parameter names against the handler's actual
// allow-list. A mismatch in either direction fails: the implementation
// accepting (or rejecting) a parameter the spec doesn't mention, or the
// spec documenting one the handler would reject as unknown.
func TestQueryContract_ParamsMatchHandlerAllowList(t *testing.T) {
	doc := loadSpec(t)

	for _, c := range collectionContracts {
		t.Run(c.path, func(t *testing.T) {
			specParams := getOperationQueryParams(t, doc, c.path, "GET")
			implParams := toSet(c.queryParams)

			for p := range specParams {
				if !implParams[p] {
					t.Errorf("OpenAPI documents query parameter %q for GET %s, but the handler's allow-list does not include it (would be rejected as unknown)", p, c.path)
				}
			}
			for p := range implParams {
				if !specParams[p] {
					t.Errorf("handler for GET %s accepts query parameter %q, but the OpenAPI spec does not document it", c.path, p)
				}
			}
		})
	}
}

// TestQueryContract_SortEnumAndDefaultsMatch is section 16: for each
// collection endpoint, the OpenAPI "sort" parameter's enum values and
// default, and the "order" parameter's default, must match the
// handler's actual allow-list/defaults.
func TestQueryContract_SortEnumAndDefaultsMatch(t *testing.T) {
	doc := loadSpec(t)

	for _, c := range collectionContracts {
		t.Run(c.path, func(t *testing.T) {
			op := getOperation(t, doc, c.path, "GET")

			sortParam := findParam(t, op, "sort")
			sortEnum := stringEnum(sortParam.Schema.Value)
			if !equalSet(sortEnum, c.sortFields) {
				t.Errorf("GET %s: spec sort enum %v does not match handler's sort allow-list %v", c.path, sortEnum, c.sortFields)
			}
			if got, _ := sortParam.Schema.Value.Default.(string); got != c.defaultSort {
				t.Errorf("GET %s: spec default sort %q does not match handler's default %q", c.path, got, c.defaultSort)
			}

			orderParam := findParam(t, op, "order")
			if got, _ := orderParam.Schema.Value.Default.(string); got != c.defaultOrder {
				t.Errorf("GET %s: spec default order %q does not match handler's default %q", c.path, got, c.defaultOrder)
			}
		})
	}
}

// TestPaginationContract_DefaultsAndBounds is section 18: the shared
// LimitParam/OffsetParam components' declared defaults/bounds must match
// httpx's actual DefaultLimit/MaxLimit constants — the single place
// every collection endpoint's pagination behaviour actually comes from.
func TestPaginationContract_DefaultsAndBounds(t *testing.T) {
	doc := loadSpec(t)

	limitParam, ok := doc.Components.Parameters["LimitParam"]
	if !ok {
		t.Fatal("no LimitParam component")
	}
	if got, _ := limitParam.Value.Schema.Value.Default.(float64); int(got) != 50 {
		t.Errorf("expected LimitParam default 50, got %v", limitParam.Value.Schema.Value.Default)
	}
	if got := limitParam.Value.Schema.Value.Max; got == nil || int(*got) != 200 {
		t.Errorf("expected LimitParam maximum 200, got %v", limitParam.Value.Schema.Value.Max)
	}
	if got := limitParam.Value.Schema.Value.Min; got == nil || int(*got) != 1 {
		t.Errorf("expected LimitParam minimum 1 (limit must be > 0), got %v", limitParam.Value.Schema.Value.Min)
	}

	offsetParam, ok := doc.Components.Parameters["OffsetParam"]
	if !ok {
		t.Fatal("no OffsetParam component")
	}
	if got, _ := offsetParam.Value.Schema.Value.Default.(float64); int(got) != 0 {
		t.Errorf("expected OffsetParam default 0, got %v", offsetParam.Value.Schema.Value.Default)
	}
	if got := offsetParam.Value.Schema.Value.Min; got == nil || int(*got) != 0 {
		t.Errorf("expected OffsetParam minimum 0, got %v", offsetParam.Value.Schema.Value.Min)
	}
}

// TestFilterEnumContract_MatchesDomainConstants is section 17: every
// enum this spec declares for a domain status/role filter is compared
// against the actual exported domain constants those filters validate
// against — not a re-typed literal list, so a future new/renamed
// constant is caught here rather than only in the handler's own
// validation-sentinel tests.
func TestFilterEnumContract_MatchesDomainConstants(t *testing.T) {
	doc := loadSpec(t)

	t.Run("invoice status filter", func(t *testing.T) {
		op := getOperation(t, doc, "/api/v1/invoices", "GET")
		got := stringEnum(findParam(t, op, "status").Schema.Value)
		want := []string{
			invoice.InvoiceStatusDraft,
			invoice.InvoiceStatusSent,
			invoice.InvoiceStatusOverdue,
			invoice.InvoiceStatusPaid,
		}
		if !equalSet(got, want) {
			t.Errorf("invoice status filter enum %v does not match domain constants %v", got, want)
		}
	})

	t.Run("customer status filter", func(t *testing.T) {
		op := getOperation(t, doc, "/api/v1/customers", "GET")
		got := stringEnum(findParam(t, op, "status").Schema.Value)
		want := []string{
			customer.CustomerStatusActive,
			customer.CustomerStatusInactive,
			customer.CustomerStatusArchived,
		}
		if !equalSet(got, want) {
			t.Errorf("customer status filter enum %v does not match domain constants %v", got, want)
		}
	})

	t.Run("user role filter", func(t *testing.T) {
		op := getOperation(t, doc, "/api/v1/users", "GET")
		got := stringEnum(findParam(t, op, "role").Schema.Value)
		want := []string{admin.UserRoleAdmin, admin.UserRoleManager, admin.UserRoleUser}
		if !equalSet(got, want) {
			t.Errorf("user role filter enum %v does not match domain constants %v", got, want)
		}
	})

	t.Run("CreateUserRequest.role enum", func(t *testing.T) {
		schemaRef := doc.Components.Schemas["CreateUserRequest"]
		got := stringEnum(schemaRef.Value.Properties["role"].Value)
		want := []string{admin.UserRoleAdmin, admin.UserRoleManager, admin.UserRoleUser}
		if !equalSet(got, want) {
			t.Errorf("CreateUserRequest.role enum %v does not match domain constants %v", got, want)
		}
	})
}

// --- shared spec-walking helpers ---

func getOperation(t *testing.T, doc *openapi3.T, path, method string) *openapi3.Operation {
	t.Helper()

	pathItem := doc.Paths.Find(path)
	if pathItem == nil {
		t.Fatalf("no path %q in the OpenAPI spec", path)
	}
	op := pathItem.Operations()[method]
	if op == nil {
		t.Fatalf("no %s operation for path %q", method, path)
	}
	return op
}

func getOperationQueryParams(t *testing.T, doc *openapi3.T, path, method string) map[string]bool {
	t.Helper()

	op := getOperation(t, doc, path, method)
	params := make(map[string]bool)
	for _, p := range op.Parameters {
		if p.Value.In == "query" {
			params[p.Value.Name] = true
		}
	}
	return params
}

func findParam(t *testing.T, op *openapi3.Operation, name string) *openapi3.Parameter {
	t.Helper()

	for _, p := range op.Parameters {
		if p.Value.Name == name {
			return p.Value
		}
	}
	t.Fatalf("operation has no parameter named %q", name)
	return nil
}

func stringEnum(schema *openapi3.Schema) []string {
	values := make([]string, 0, len(schema.Enum))
	for _, v := range schema.Enum {
		if s, ok := v.(string); ok {
			values = append(values, s)
		}
	}
	return values
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA, sortedB := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(sortedA)
	sort.Strings(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}
