package app

import (
	"testing"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"
)

// strPtr and intPtr exist because every *string/*int field on the
// PATCH-style request DTOs below needs a non-nil pointer to a
// fully-populated value — a zero-value (nil) pointer would marshal that
// field away entirely (it's how these DTOs represent "omitted, leave
// unchanged"), which would hide it from schema validation and defeat
// the whole point of this test: proving the DTO's full field set still
// matches the spec.
func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// TestRequestContract_MatchesOpenAPISchema is Milestone 8 Part 5 section
// 8: it marshals an actual, fully-populated instance of every listed
// request DTO and validates the result against the corresponding
// OpenAPI component schema. Because every one of these DTOs is decoded
// by httpx.DecodeJSON (DisallowUnknownFields) and every request schema
// in the spec now declares additionalProperties: false to match, this
// also transitively proves the spec's declared field set for each DTO
// is neither missing a field the Go struct has, nor documenting one it
// doesn't (see TestRequestContract_AdditionalPropertiesAreRejected for
// the direct proof that the "missing from spec" direction is actually
// enforced, not just assumed).
func TestRequestContract_MatchesOpenAPISchema(t *testing.T) {
	doc := loadSpec(t)

	t.Run("LoginRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "LoginRequest", admin.LoginRequest{
			Email:    "ada@example.com",
			Password: "correct-horse-battery-staple",
		})
	})

	t.Run("RegisterRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "RegisterRequest", admin.RegisterRequest{
			Organisation: admin.RegisterOrganisationRequest{Name: "Acme Consulting Ltd"},
			User: admin.RegisterUserRequest{
				Name:     "Ada Lovelace",
				Email:    "ada@example.com",
				Password: "correct-horse-battery-staple",
			},
		})
	})

	t.Run("CreateUserRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "CreateUserRequest", admin.CreateUserRequest{
			Name:     "Grace Hopper",
			Email:    "grace@example.com",
			Password: "another-strong-password",
			Role:     admin.UserRoleManager,
		})
	})

	t.Run("UpdateOrganisationRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "UpdateOrganisationRequest", admin.UpdateOrganisationRequest{
			Name:       strPtr("Acme Consulting Ltd"),
			Email:      strPtr("hello@acme.example"),
			Phone:      strPtr("+44 20 7946 0958"),
			Website:    strPtr("https://acme.example.com"),
			Address:    strPtr("1 High Street"),
			City:       strPtr("London"),
			State:      strPtr("Greater London"),
			PostalCode: strPtr("SW1A 1AA"),
			Country:    strPtr("GB"),
			TaxID:      strPtr("GB123456789"),
		})
	})

	t.Run("UpdateSettingsRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "UpdateSettingsRequest", admin.UpdateSettingsRequest{
			Currency:      strPtr("GBP"),
			PaymentTerms:  intPtr(14),
			InvoicePrefix: strPtr("INV-"),
		})
	})

	t.Run("CreateCustomerRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "CreateCustomerRequest", customer.CreateCustomerRequest{
			Name:        "Contoso Ltd",
			Email:       "billing@contoso.example",
			Phone:       "+1 555 0100",
			CompanyName: "Contoso Ltd",
			TaxID:       "US123456789",
		})
	})

	t.Run("UpsertBillingAddressRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "UpsertBillingAddressRequest", customer.UpsertBillingAddressRequest{
			Street:     "1 High Street",
			City:       "London",
			State:      "Greater London",
			PostalCode: "SW1A 1AA",
			Country:    "GB",
		})
	})

	t.Run("CreateProductRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "CreateProductRequest", product.CreateProductRequest{
			Name:        "Consulting (half day)",
			Description: "Half a day of on-site consulting",
			SKU:         "CONS-HD",
			Price:       50000,
			Category:    "Services",
		})
	})

	t.Run("CreateInvoiceRequest", func(t *testing.T) {
		productID := "3fa85f64-5717-4562-b3fc-2c963f66afa6"
		validateAgainstSchema(t, doc, "CreateInvoiceRequest", invoice.CreateInvoiceRequest{
			CustomerID: "11111111-1111-1111-1111-111111111111",
			IssueDate:  "2026-09-19",
			DueDate:    "2026-10-19",
			Lines: []invoice.CreateInvoiceLineRequest{
				{
					ProductID:   &productID,
					Description: "Consulting (half day)",
					Quantity:    2,
					UnitPrice:   50000,
					VATRate:     20,
				},
			},
			Notes: "Thank you for your business.",
		})
	})

	t.Run("CreatePaymentHTTPRequest", func(t *testing.T) {
		validateAgainstSchema(t, doc, "CreatePaymentHTTPRequest", invoice.CreatePaymentHTTPRequest{
			Amount:        40000,
			PaymentMethod: "bank_transfer",
			PaymentDate:   "2026-09-19",
			Reference:     "TX-90210",
			Notes:         "Paid via wire transfer.",
		})
	})
}

// TestRequestContract_AdditionalPropertiesAreRejected proves the
// additionalProperties: false declared on every request schema (added in
// Milestone 8 Part 5 specifically to make this possible) actually
// rejects a field the schema doesn't know about — the concrete drift
// scenario section 8 calls out ("Go adds json:newField but OpenAPI
// forgets it"). Without this test, TestRequestContract_MatchesOpenAPISchema
// above could only ever prove "the fields present validate", never "no
// field is missing from the spec" — a schema with no
// additionalProperties restriction at all would let literally anything
// through.
func TestRequestContract_AdditionalPropertiesAreRejected(t *testing.T) {
	doc := loadSpec(t)

	type loginRequestWithDrift struct {
		admin.LoginRequest
		UnexpectedNewField string `json:"unexpectedNewField"`
	}

	mustNotValidate(t, doc, "LoginRequest", loginRequestWithDrift{
		LoginRequest:       admin.LoginRequest{Email: "ada@example.com", Password: "x"},
		UnexpectedNewField: "this field does not exist in the OpenAPI schema",
	})
}
