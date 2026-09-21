package app

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// schemaProperty is one (schema, property) pair this file checks the
// declared type/format of — a small, explicit table rather than a
// reflection-based scan of every schema, since only the money/date/time
// fields listed in Milestone 8 Part 5 sections 19-20 need this level of
// scrutiny.
type schemaProperty struct {
	schema   string
	property string
}

// moneyFields is section 20's explicit list of every field this API
// represents as an integer minor-unit amount.
var moneyFields = []schemaProperty{
	{"ProductResponse", "price"},
	{"CreateProductRequest", "price"},
	{"CreateInvoiceLineRequest", "unitPrice"},
	{"InvoiceLineResponse", "unitPrice"},
	{"InvoiceLineResponse", "vatAmount"},
	{"InvoiceLineResponse", "total"},
	{"InvoiceResponse", "subtotal"},
	{"InvoiceResponse", "vatTotal"},
	{"InvoiceResponse", "total"},
	{"InvoiceResponse", "amountPaid"},
	{"InvoiceResponse", "amountOutstanding"},
	{"InvoiceListItemResponse", "subtotal"},
	{"InvoiceListItemResponse", "vatTotal"},
	{"InvoiceListItemResponse", "total"},
	{"InvoiceListItemResponse", "amountPaid"},
	{"InvoiceListItemResponse", "amountOutstanding"},
	{"CreatePaymentHTTPRequest", "amount"},
	{"PaymentResponse", "amount"},
}

// TestMoneyContract_FieldsAreIntegerMinorUnits is Milestone 8 Part 5
// section 20: it asserts every listed money field is declared as
// `type: integer` (never number/string) in the maintained spec — the
// structural guarantee that "money is always an integer number of minor
// units, never a float" actually holds in the document, not just in
// prose.
func TestMoneyContract_FieldsAreIntegerMinorUnits(t *testing.T) {
	doc := loadSpec(t)

	for _, f := range moneyFields {
		t.Run(f.schema+"."+f.property, func(t *testing.T) {
			prop := propertySchema(t, doc, f.schema, f.property)
			if !prop.Type.Is("integer") {
				t.Errorf("expected %s.%s to be type integer, got %v", f.schema, f.property, prop.Type)
			}
		})
	}
}

// TestMoneyContract_RejectsFractionalAmount is the regression proof
// behind the assertion above: it feeds a genuinely fractional value
// through the real schema (not just inspecting the declared type) and
// confirms it's rejected, using InvoiceResponse.subtotal as the
// representative case.
//
// Limitation (see this package's own Part 5 report): this proves the
// spec's `type: integer` constraint is live and enforced, not that a
// hypothetical future change of a Go field from int64 to float64 would
// itself be caught — a whole-number float64 (e.g. 100000.0) marshals to
// JSON identically to an int64 (100000), so encoding/json output alone
// can't distinguish the two. Only a value that happens to carry a
// fractional part would ever surface that particular drift on the wire.
func TestMoneyContract_RejectsFractionalAmount(t *testing.T) {
	doc := loadSpec(t)

	base := validInvoiceResponseJSON()
	base["subtotal"] = 100000.5

	schemaRef := doc.Components.Schemas["InvoiceResponse"]
	if err := schemaRef.Value.VisitJSON(base, openapi3.EnableJSONSchema2020()); err == nil {
		t.Error("expected a fractional subtotal to be rejected by the InvoiceResponse schema, but it validated")
	}
}

// dateFields and dateTimeFields are section 19's explicit lists of every
// calendar-date and timestamp field.
var dateFields = []schemaProperty{
	{"CreateInvoiceRequest", "issueDate"},
	{"CreateInvoiceRequest", "dueDate"},
	{"InvoiceResponse", "issueDate"},
	{"InvoiceResponse", "dueDate"},
	{"InvoiceListItemResponse", "issueDate"},
	{"InvoiceListItemResponse", "dueDate"},
	{"CreatePaymentHTTPRequest", "paymentDate"},
	{"PaymentResponse", "paymentDate"},
}

var dateTimeFields = []schemaProperty{
	{"UserResponse", "createdAt"},
	{"UserResponse", "updatedAt"},
	{"UserResponse", "lastLogin"},
	{"LoginResponse", "expiresAt"},
	{"InvoiceResponse", "createdAt"},
	{"InvoiceResponse", "updatedAt"},
	{"InvoiceResponse", "sentAt"},
	{"InvoiceListItemResponse", "createdAt"},
	{"InvoiceListItemResponse", "sentAt"},
}

// TestDateTimeContract_FieldsHaveCorrectFormat is section 19: calendar
// dates must be `type: string, format: date`; timestamps must be
// `type: string, format: date-time`.
func TestDateTimeContract_FieldsHaveCorrectFormat(t *testing.T) {
	doc := loadSpec(t)

	for _, f := range dateFields {
		t.Run("date/"+f.schema+"."+f.property, func(t *testing.T) {
			prop := propertySchema(t, doc, f.schema, f.property)
			if !prop.Type.Is("string") || prop.Format != "date" {
				t.Errorf("expected %s.%s to be type string, format date, got type %v format %q", f.schema, f.property, prop.Type, prop.Format)
			}
		})
	}

	for _, f := range dateTimeFields {
		t.Run("date-time/"+f.schema+"."+f.property, func(t *testing.T) {
			prop := propertySchema(t, doc, f.schema, f.property)
			if !prop.Type.Is("string") || prop.Format != "date-time" {
				t.Errorf("expected %s.%s to be type string, format date-time, got type %v format %q", f.schema, f.property, prop.Type, prop.Format)
			}
		})
	}
}

// TestDateTimeContract_RejectsMalformedDate proves format:date is
// actually enforced (not merely declared) by kin-openapi's JSON Schema
// 2020-12 validator, using InvoiceResponse.issueDate as the
// representative case: a value that is a syntactically valid JSON
// string but not a YYYY-MM-DD date is rejected.
func TestDateTimeContract_RejectsMalformedDate(t *testing.T) {
	doc := loadSpec(t)

	base := validInvoiceResponseJSON()
	base["issueDate"] = "19-09-2026"

	schemaRef := doc.Components.Schemas["InvoiceResponse"]
	if err := schemaRef.Value.VisitJSON(base, openapi3.EnableJSONSchema2020()); err == nil {
		t.Error("expected a malformed issueDate to be rejected by the InvoiceResponse schema, but it validated")
	}
}

// currencyFields is section 21: every field carrying a currency code.
var currencyFields = []schemaProperty{
	{"InvoiceResponse", "currency"},
	{"InvoiceListItemResponse", "currency"},
	{"SettingsResponse", "currency"},
	{"UpdateSettingsRequest", "currency"},
}

// TestCurrencyContract_FieldsAreThreeLetterStrings is section 21: this
// deliberately does NOT test the live-settings-vs-immutable-snapshot
// business rule (existing application tests, e.g.
// TestApp_InvoiceLifecycle_DraftSentPaid and
// internal/invoice's currency-resolution tests, already cover that) —
// only that every currency field is shaped like a currency code in the
// spec: a plain string matching ^[A-Z]{3}$.
func TestCurrencyContract_FieldsAreThreeLetterStrings(t *testing.T) {
	doc := loadSpec(t)

	for _, f := range currencyFields {
		t.Run(f.schema+"."+f.property, func(t *testing.T) {
			prop := propertySchema(t, doc, f.schema, f.property)
			if !prop.Type.Is("string") {
				t.Errorf("expected %s.%s to be type string, got %v", f.schema, f.property, prop.Type)
			}
			if prop.Pattern != "^[A-Z]{3}$" {
				t.Errorf(`expected %s.%s to have pattern "^[A-Z]{3}$", got %q`, f.schema, f.property, prop.Pattern)
			}
		})
	}

	// Both invoice representations must expose currency at all (section
	// 21: "invoice responses expose currency", "list invoice responses
	// expose currency") — checked here as "is it in the required set",
	// not merely present as an optional property.
	for _, schemaName := range []string{"InvoiceResponse", "InvoiceListItemResponse"} {
		schemaRef := doc.Components.Schemas[schemaName]
		required := make(map[string]bool, len(schemaRef.Value.Required))
		for _, name := range schemaRef.Value.Required {
			required[name] = true
		}
		if !required["currency"] {
			t.Errorf("expected %s to require currency", schemaName)
		}
	}
}

// propertySchema resolves schemaName.propertyName to its *openapi3.Schema,
// failing the test immediately (not returning an error) if either the
// schema or the property doesn't exist — every caller in this file wants
// exactly that: a missing property is itself the drift being tested for.
func propertySchema(t *testing.T, doc *openapi3.T, schemaName, propertyName string) *openapi3.Schema {
	t.Helper()

	schemaRef, ok := doc.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("no component schema named %q", schemaName)
	}

	propRef, ok := schemaRef.Value.Properties[propertyName]
	if !ok {
		t.Fatalf("schema %q has no property %q", schemaName, propertyName)
	}

	return propRef.Value
}

// validInvoiceResponseJSON returns a generic (map[string]any) value that
// satisfies the InvoiceResponse schema in full — a shared fixture for
// the negative-case tests in this file, which each mutate exactly one
// field away from valid rather than re-declaring the whole object.
func validInvoiceResponseJSON() map[string]any {
	const raw = `{
		"id": "22222222-2222-2222-2222-222222222222",
		"organisationId": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		"customerId": "11111111-1111-1111-1111-111111111111",
		"invoiceNumber": "INV-1",
		"issueDate": "2026-09-19",
		"dueDate": "2026-10-19",
		"currency": "GBP",
		"subtotal": 100000,
		"vatTotal": 20000,
		"total": 120000,
		"amountPaid": 0,
		"amountOutstanding": 120000,
		"status": "draft",
		"lines": [],
		"createdAt": "2026-09-19T09:00:00Z",
		"updatedAt": "2026-09-19T09:00:00Z"
	}`

	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		panic(err)
	}
	return value
}
