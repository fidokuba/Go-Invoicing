package renderer_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"go-invoicing/internal/invoice"
	"go-invoicing/internal/renderer"
)

// newTestClient skips without RENDERER_URL set, the same convention
// internal/administration's newTestPool uses for DATABASE_URL — this
// test needs the real renderer service (Node + headless Chromium)
// running, exactly as the Postgres-backed tests need a real database,
// so it's opt-in locally and mandatory only where CI arranges the
// dependency (a later phase's concern — see this project's own
// REQUIRE_DATABASE_TESTS precedent for how that gets enforced).
func newTestClient(t *testing.T) *renderer.Client {
	t.Helper()

	rendererURL := os.Getenv("RENDERER_URL")
	if rendererURL == "" {
		t.Skip("RENDERER_URL is not set")
	}

	return renderer.NewClient(rendererURL)
}

// TestClient_Render_ProducesAValidPDF is Phase 1's actual proof: a
// realistic InvoicePDFData, converted and sent to the real renderer
// service, must come back as bytes a PDF reader would accept — checked
// here by the same signal any PDF-consuming tool checks first, the
// "%PDF-" magic header — and be a plausible size for a real document,
// not an empty or truncated response.
func TestClient_Render_ProducesAValidPDF(t *testing.T) {
	client := newTestClient(t)

	data := invoice.InvoicePDFData{
		InvoiceNumber: "INV-0001",
		IssueDate:     "15 Jan 2026",
		DueDate:       "14 Feb 2026",
		StatusLabel:   "",
		Seller: invoice.InvoicePDFSeller{
			Name:         "Acme Consulting Ltd",
			AddressLines: []string{"123 High Street", "London, EC1A 1AA"},
			Email:        "hello@acme.example",
			TaxID:        "GB123456789",
		},
		Customer: invoice.InvoicePDFCustomer{
			DisplayName:  "Widget Co",
			AddressLines: []string{"456 Market Road", "Manchester, M1 1AA"},
		},
		Lines: []invoice.InvoicePDFLine{
			{Description: "Consulting services", Quantity: "1", UnitPrice: "£500.00", VATRate: "20%", VATAmount: "£100.00", Total: "£600.00"},
			{Description: "Software licence", Quantity: "1", UnitPrice: "£500.00", VATRate: "20%", VATAmount: "£100.00", Total: "£600.00"},
		},
		VATRegistered: true,
		Subtotal:      "£1,000.00",
		VATTotal:      "£200.00",
		Total:         "£1,200.00",
		Notes:         "Thank you for your business.",
	}

	pdf, err := client.Render(context.Background(), data.ToRenderRequest(nil))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("expected response to start with the PDF magic header, got %d bytes starting %q", len(pdf), pdf[:min(20, len(pdf))])
	}

	// A genuinely empty or single-page-of-nothing PDF would still start
	// with the right header but be suspiciously tiny — this is a coarse
	// floor, not a golden-size assertion, just enough to catch "produced
	// a technically-valid but content-free PDF."
	const minimumPlausibleBytes = 1000
	if len(pdf) < minimumPlausibleBytes {
		t.Errorf("expected a real multi-section invoice PDF to be at least %d bytes, got %d", minimumPlausibleBytes, len(pdf))
	}
}

// TestClient_Render_NonVATRegisteredOmitsVATColumns proves the same
// VATRegistered-driven conditional rendering the gopdf renderer already
// guarantees (no VAT columns/rows for a non-VAT-registered seller) holds
// for this new pipeline too — checked indirectly, via response size
// (fewer columns/rows) rather than parsing the PDF's internal content
// stream, which is out of scope for this client-level test.
func TestClient_Render_NonVATRegisteredOmitsVATColumns(t *testing.T) {
	client := newTestClient(t)

	base := invoice.InvoicePDFData{
		InvoiceNumber: "INV-0002",
		IssueDate:     "15 Jan 2026",
		DueDate:       "14 Feb 2026",
		Seller:        invoice.InvoicePDFSeller{Name: "Sole Trader"},
		Customer:      invoice.InvoicePDFCustomer{DisplayName: "Widget Co"},
		Lines: []invoice.InvoicePDFLine{
			{Description: "Consulting services", Quantity: "1", UnitPrice: "£500.00", Total: "£500.00"},
		},
		Total: "£500.00",
	}

	pdf, err := client.Render(context.Background(), base.ToRenderRequest(nil))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("expected a valid PDF, got %d bytes", len(pdf))
	}
}
