package app

import (
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"go-invoicing/internal/invoice"
)

// Milestone 13 Part 1: POST /api/v1/invoices/{id}/payments's documented
// Idempotency-Key contract must match what the server actually enforces
// and returns.

const paymentsPath = "/api/v1/invoices/{id}/payments"

func TestIdempotencyContract_PaymentCreationRequiresTheHeader(t *testing.T) {
	doc := loadSpec(t)
	op := getOperation(t, doc, paymentsPath, "POST")

	var header *openapi3.Parameter
	for _, p := range op.Parameters {
		if p.Value.In == "header" && p.Value.Name == "Idempotency-Key" {
			header = p.Value
		}
	}
	if header == nil {
		t.Fatal("expected POST /invoices/{id}/payments to declare an Idempotency-Key header parameter")
	}
	if !header.Required {
		t.Error("expected Idempotency-Key to be required")
	}

	schema := header.Schema.Value
	if schema.MinLength != 16 || schema.MaxLength == nil || *schema.MaxLength != 128 {
		t.Errorf("expected minLength 16 / maxLength 128, got %d / %v", schema.MinLength, schema.MaxLength)
	}

	// The documented pattern and the server's validator must agree.
	pattern := regexp.MustCompile(schema.Pattern)
	for _, key := range []string{
		strings.Repeat("a", 15), strings.Repeat("a", 16), strings.Repeat("a", 128), strings.Repeat("a", 129),
		"8e03978e-40d5-43e8-bc93-6894a57f9324", "a.b_c~d:e-f012345", "has a space in it!",
		`"quoted-key-000000"`, "slash/not-allowed-x", "comma,not-allowed-x", "unicode-é-not-allowed",
	} {
		documented := pattern.MatchString(key)
		enforced := invoice.ValidateIdempotencyKey(key) == nil
		if documented != enforced {
			t.Errorf("key %q: spec pattern says valid=%v, server says valid=%v", key, documented, enforced)
		}
	}

	// No other operation uses the header.
	for path, pathItem := range doc.Paths.Map() {
		for method, other := range pathItem.Operations() {
			if path == paymentsPath && method == "POST" {
				continue
			}
			for _, p := range other.Parameters {
				if p.Value.In == "header" && p.Value.Name == "Idempotency-Key" {
					t.Errorf("%s %s unexpectedly declares Idempotency-Key", method, path)
				}
			}
		}
	}
}

func TestIdempotencyContract_ReplayHeaderAndConflictCodeDocumented(t *testing.T) {
	doc := loadSpec(t)
	op := getOperation(t, doc, paymentsPath, "POST")

	created := op.Responses.Status(201)
	if created == nil {
		t.Fatal("no 201 response declared")
	}
	replayed, ok := created.Value.Headers["Idempotent-Replayed"]
	if !ok {
		t.Fatal("expected the 201 response to document the Idempotent-Replayed header")
	}
	if enum := replayed.Value.Schema.Value.Enum; len(enum) != 1 || enum[0] != "true" {
		t.Errorf(`expected Idempotent-Replayed to be documented as exactly "true", got %v`, enum)
	}

	conflict := op.Responses.Status(409)
	if conflict == nil {
		t.Fatal("no 409 response declared")
	}
	if !strings.Contains(conflict.Value.Content.Get("application/json").Example.(map[string]any)["error"].(map[string]any)["code"].(string), "idempotency_key_reused") {
		t.Error("expected the 409 example to show the idempotency_key_reused code")
	}
	if !strings.Contains(*conflict.Value.Description, "idempotency_key_reused") {
		t.Error("expected the 409 description to document idempotency_key_reused")
	}
}
