package app

import (
	"encoding/json"
	"testing"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/httpx"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"
)

// TestResponseContract_MatchesOpenAPISchema is Milestone 8 Part 5 section
// 9: it builds an actual, fully-populated instance of every listed
// response DTO and validates its real json.Marshal output against the
// corresponding OpenAPI component schema. Every field is set to a
// non-zero value deliberately — a zero-value field the Go type happens
// to omit (a nil *string with omitempty, for instance) would silently
// hide a real drift the same way an untested field would, so this
// exercises every field the type has, not just the ones a handler
// happened to populate in some other test's fixture.
//
// The response DTOs' own `to...Response` mapping functions
// (toUserResponse, toInvoiceResponse, ...) are unexported to their
// packages, so this constructs each Response struct directly — still
// the real, exported wire type and its real json tags, just without
// going through the private mapping step, which is the same DTO
// json.Marshal ultimately serializes regardless of how it was built.
func TestResponseContract_MatchesOpenAPISchema(t *testing.T) {
	doc := loadSpec(t)

	lastLogin := "2026-09-19T09:00:00Z"
	user := admin.UserResponse{
		ID:             "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		OrganisationID: "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		Name:           "Ada Lovelace",
		Email:          "ada@example.com",
		Role:           admin.UserRoleAdmin,
		IsActive:       true,
		LastLogin:      &lastLogin,
		CreatedAt:      "2026-01-15T10:00:00Z",
		UpdatedAt:      "2026-01-15T10:00:00Z",
	}

	t.Run("UserResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "UserResponse", user)
	})

	t.Run("LoginResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "LoginResponse", admin.LoginResponse{
			Token:     "8f3b1e2c9d4a4f6e8b0a1c2d3e4f5061",
			ExpiresAt: "2026-09-20T09:00:00Z",
			User:      user,
		})
	})

	email, phone, website := "hello@acme.example", "+44 20 7946 0958", "https://acme.example.com"
	address, city, state, postalCode, country, taxID := "1 High Street", "London", "Greater London", "SW1A 1AA", "GB", "GB123456789"
	organisation := admin.OrganisationResponse{
		ID:         "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		Name:       "Acme Consulting Ltd",
		Email:      &email,
		Phone:      &phone,
		Website:    &website,
		Address:    &address,
		City:       &city,
		State:      &state,
		PostalCode: &postalCode,
		Country:    &country,
		TaxID:      &taxID,
		CreatedAt:  "2026-01-15T10:00:00Z",
		UpdatedAt:  "2026-01-15T10:00:00Z",
	}

	t.Run("OrganisationResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "OrganisationResponse", organisation)
	})

	t.Run("RegisterResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "RegisterResponse", admin.RegisterResponse{
			Organisation: organisation,
			User:         user,
		})
	})

	t.Run("SettingsResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "SettingsResponse", admin.SettingsResponse{
			Currency:      "GBP",
			PaymentTerms:  30,
			InvoicePrefix: "INV-",
		})
	})

	customerEmail, customerPhone, companyName, customerTaxID := "billing@contoso.example", "+1 555 0100", "Contoso Ltd", "US123456789"

	t.Run("CustomerResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "CustomerResponse", customer.CustomerResponse{
			ID:             "11111111-1111-1111-1111-111111111111",
			OrganisationID: "7c9e6679-7425-40de-944b-e07fc1f90ae7",
			Name:           "Contoso Ltd",
			Email:          &customerEmail,
			Phone:          &customerPhone,
			CompanyName:    &companyName,
			TaxID:          &customerTaxID,
			Status:         customer.CustomerStatusActive,
			CreatedAt:      "2026-01-15T10:00:00Z",
			UpdatedAt:      "2026-01-15T10:00:00Z",
		})
	})

	t.Run("AddressResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "AddressResponse", customer.AddressResponse{
			ID:         "55555555-5555-5555-5555-555555555555",
			CustomerID: "11111111-1111-1111-1111-111111111111",
			Type:       customer.AddressTypeBilling,
			Street:     "1 High Street",
			City:       "London",
			State:      "Greater London",
			PostalCode: "SW1A 1AA",
			Country:    "GB",
			CreatedAt:  "2026-01-15T10:00:00Z",
			UpdatedAt:  "2026-01-15T10:00:00Z",
		})
	})

	productDescription, productCategory := "Half a day of on-site consulting", "Services"

	t.Run("ProductResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "ProductResponse", product.ProductResponse{
			ID:             "66666666-6666-6666-6666-666666666666",
			OrganisationID: "7c9e6679-7425-40de-944b-e07fc1f90ae7",
			Name:           "Consulting (half day)",
			Description:    &productDescription,
			SKU:            "CONS-HD",
			Price:          50000,
			Category:       &productCategory,
			IsActive:       true,
			CreatedAt:      "2026-01-15T10:00:00Z",
			UpdatedAt:      "2026-01-15T10:00:00Z",
		})
	})

	productID := "66666666-6666-6666-6666-666666666666"
	sentAt := "2026-09-19T09:05:00Z"
	notes := "Thank you for your business."

	fullInvoice := invoice.InvoiceResponse{
		ID:                "22222222-2222-2222-2222-222222222222",
		OrganisationID:    "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		CustomerID:        "11111111-1111-1111-1111-111111111111",
		InvoiceNumber:     "INV-1",
		IssueDate:         "2026-09-19",
		DueDate:           "2026-10-19",
		Currency:          "GBP",
		Subtotal:          100000,
		VATTotal:          20000,
		Total:             120000,
		AmountPaid:        40000,
		AmountOutstanding: 80000,
		Status:            invoice.InvoiceStatusSent,
		SentAt:            &sentAt,
		Notes:             &notes,
		Lines: []invoice.InvoiceLineResponse{
			{
				ID:          "33333333-3333-3333-3333-333333333333",
				ProductID:   &productID,
				Description: "Consulting (half day)",
				Quantity:    2,
				UnitPrice:   50000,
				VATRate:     20,
				VATAmount:   20000,
				Total:       120000,
			},
		},
		CreatedAt: "2026-09-19T09:00:00Z",
		UpdatedAt: "2026-09-19T09:05:00Z",
	}

	t.Run("InvoiceResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "InvoiceResponse", fullInvoice)
	})

	listItem := invoice.InvoiceListItemResponse{
		ID:                "22222222-2222-2222-2222-222222222222",
		InvoiceNumber:     "INV-1",
		CustomerID:        "11111111-1111-1111-1111-111111111111",
		IssueDate:         "2026-09-19",
		DueDate:           "2026-10-19",
		Status:            invoice.InvoiceStatusSent,
		Currency:          "GBP",
		Subtotal:          100000,
		VATTotal:          20000,
		Total:             120000,
		AmountPaid:        40000,
		AmountOutstanding: 80000,
		SentAt:            &sentAt,
		CreatedAt:         "2026-09-19T09:00:00Z",
		UpdatedAt:         "2026-09-19T09:05:00Z",
	}

	t.Run("InvoiceListItemResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "InvoiceListItemResponse", listItem)
	})

	reference := "TX-90210"
	paymentNotes := "Paid via wire transfer."

	t.Run("PaymentResponse", func(t *testing.T) {
		validateAgainstSchema(t, doc, "PaymentResponse", invoice.PaymentResponse{
			ID:            "44444444-4444-4444-4444-444444444444",
			InvoiceID:     "22222222-2222-2222-2222-222222222222",
			Amount:        40000,
			PaymentMethod: "bank_transfer",
			PaymentDate:   "2026-09-19",
			Reference:     &reference,
			Notes:         &paymentNotes,
			CreatedAt:     "2026-09-19T09:05:00Z",
			UpdatedAt:     "2026-09-19T09:05:00Z",
		})
	})

	t.Run("paginated list response (CustomerListResponse)", func(t *testing.T) {
		listResponse := httpx.NewListResponse([]customer.CustomerResponse{
			{
				ID:             "11111111-1111-1111-1111-111111111111",
				OrganisationID: "7c9e6679-7425-40de-944b-e07fc1f90ae7",
				Name:           "Contoso Ltd",
				Status:         customer.CustomerStatusActive,
				CreatedAt:      "2026-01-15T10:00:00Z",
				UpdatedAt:      "2026-01-15T10:00:00Z",
			},
		}, 50, 0, 1)

		validateAgainstSchema(t, doc, "CustomerListResponse", listResponse)
	})

	t.Run("paginated list response, empty items (InvoiceListResponse)", func(t *testing.T) {
		// httpx.NewListResponse defaults Items to []T{}, never nil — the
		// pagination contract (section 18) requires "items": [], not
		// "items": null, for an empty page.
		empty := httpx.NewListResponse[invoice.InvoiceListItemResponse](nil, 50, 0, 0)

		data, err := json.Marshal(empty)
		if err != nil {
			t.Fatalf("marshal empty list response: %v", err)
		}
		if string(data) != `{"items":[],"pagination":{"limit":50,"offset":0,"total":0}}` {
			t.Errorf(`expected "items" to serialize as [] not null, got: %s`, data)
		}

		validateAgainstSchema(t, doc, "InvoiceListResponse", empty)
	})
}

// TestResponseContract_ListItemMustNotHaveLines is Milestone 8 Part 5
// section 10's explicit regression test protecting the Part 3 amendment:
// it proves the OpenAPI schema itself — not just the current Go struct —
// would reject a "lines" property on an invoice list row, so a future
// change that accidentally reintroduces one (either on the Go DTO or by
// someone hand-editing the spec to add it back) fails here immediately.
//
// This is deliberately independent of
// TestInvoiceHandler_List_UsesSummaryRepresentation (internal/invoice's
// own Part 3 amendment test): that one proves the real HTTP handler's
// JSON has no "lines" key; this one proves the OpenAPI *contract* itself
// — additionalProperties: false plus no "lines" property declared — would
// catch it even if the Go struct alone were changed without anyone
// touching the handler test.
func TestResponseContract_ListItemMustNotHaveLines(t *testing.T) {
	doc := loadSpec(t)

	schemaRef, ok := doc.Components.Schemas["InvoiceListItemResponse"]
	if !ok {
		t.Fatal(`no component schema named "InvoiceListItemResponse"`)
	}

	if _, hasLines := schemaRef.Value.Properties["lines"]; hasLines {
		t.Fatal(`InvoiceListItemResponse schema must not declare a "lines" property`)
	}

	type invoiceListItemWithDrift struct {
		invoice.InvoiceListItemResponse
		Lines []invoice.InvoiceLineResponse `json:"lines"`
	}

	mustNotValidate(t, doc, "InvoiceListItemResponse", invoiceListItemWithDrift{
		InvoiceListItemResponse: invoice.InvoiceListItemResponse{
			ID:            "22222222-2222-2222-2222-222222222222",
			InvoiceNumber: "INV-1",
			CustomerID:    "11111111-1111-1111-1111-111111111111",
			IssueDate:     "2026-09-19",
			DueDate:       "2026-10-19",
			Status:        invoice.InvoiceStatusSent,
			Currency:      "GBP",
			CreatedAt:     "2026-09-19T09:00:00Z",
			UpdatedAt:     "2026-09-19T09:00:00Z",
		},
		Lines: []invoice.InvoiceLineResponse{{ID: "x", Description: "should never be here"}},
	})
}

// TestResponseContract_FullInvoiceMustHaveLines is the other half of
// section 10: the full InvoiceResponse schema must still require
// "lines" — if a future edit ever removed it from the schema's required
// list or dropped the property entirely, this fails immediately, rather
// than only being noticed the next time someone happens to look at the
// spec by eye.
func TestResponseContract_FullInvoiceMustHaveLines(t *testing.T) {
	doc := loadSpec(t)

	schemaRef, ok := doc.Components.Schemas["InvoiceResponse"]
	if !ok {
		t.Fatal(`no component schema named "InvoiceResponse"`)
	}

	if _, hasLines := schemaRef.Value.Properties["lines"]; !hasLines {
		t.Fatal(`InvoiceResponse schema must declare a "lines" property`)
	}

	required := make(map[string]bool, len(schemaRef.Value.Required))
	for _, name := range schemaRef.Value.Required {
		required[name] = true
	}
	if !required["lines"] {
		t.Error(`InvoiceResponse schema must list "lines" as required`)
	}
}
