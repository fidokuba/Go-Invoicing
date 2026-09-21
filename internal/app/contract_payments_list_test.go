package app

import (
	"testing"

	"go-invoicing/internal/invoice"
)

// TestPaymentsListContract_IsABareArrayNotAPaginatedEnvelope is
// Milestone 8 Part 5 section 31: GET /api/v1/invoices/{id}/payments is a
// bounded child-resource collection (InvoiceHandler.GetPayments writes
// `[]PaymentResponse` directly via httpx.WriteJSON — no
// httpx.NewListResponse call at all, unlike every other list endpoint),
// and Part 4 deliberately kept that shape rather than "fixing" it into
// the {items, pagination} envelope for consistency. This proves the
// maintained spec still documents that actual shape: a plain array
// schema, not a $ref to any of the *ListResponse schemas, and not an
// object with "items"/"pagination" keys.
func TestPaymentsListContract_IsABareArrayNotAPaginatedEnvelope(t *testing.T) {
	doc := loadSpec(t)

	op := getOperation(t, doc, "/api/v1/invoices/{id}/payments", "GET")
	response := op.Responses.Value("200")
	if response == nil {
		t.Fatal("no 200 response declared for GET /api/v1/invoices/{id}/payments")
	}

	mediaType := response.Value.Content.Get("application/json")
	if mediaType == nil {
		t.Fatal("expected an application/json response")
	}

	schema := mediaType.Schema.Value
	if !schema.Type.Is("array") {
		t.Fatalf("expected a bare array schema, got type %v", schema.Type)
	}
	if schema.Items == nil {
		t.Fatal("expected the array's items schema to be declared")
	}

	// The items schema must be (a $ref to) PaymentResponse specifically —
	// resolved, so comparing by identity against the named component is
	// exactly the same object.
	paymentResponseSchema := doc.Components.Schemas["PaymentResponse"]
	if schema.Items.Value != paymentResponseSchema.Value {
		t.Error("expected the array's items to be the PaymentResponse schema")
	}

	// And prove an actual []PaymentResponse (the real Go return type of
	// InvoiceService.GetPayments once mapped by the handler) validates
	// against exactly this array schema, not some other shape.
	payments := []invoice.PaymentResponse{
		{
			ID:            "44444444-4444-4444-4444-444444444444",
			InvoiceID:     "22222222-2222-2222-2222-222222222222",
			Amount:        40000,
			PaymentMethod: "bank_transfer",
			PaymentDate:   "2026-09-19",
			CreatedAt:     "2026-09-19T09:05:00Z",
			UpdatedAt:     "2026-09-19T09:05:00Z",
		},
	}

	validateAgainstArraySchema(t, schema, payments)
}
