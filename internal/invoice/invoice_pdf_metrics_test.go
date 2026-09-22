package invoice

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"go-invoicing/internal/metrics"
)

// pdfServiceWithMetrics mirrors testFixture.pdfService (invoice_service_test.go)
// but wires a real *metrics.Metrics instead of that helper's hard-coded
// nil, so these tests can scrape and assert on what Generate actually
// recorded.
func (f *testFixture) pdfServiceWithMetrics(m *metrics.Metrics) *InvoicePDFService {
	return NewInvoicePDFService(
		f.repository,
		f.paymentRepository,
		f.organisationRepository,
		f.customerRepository,
		f.addressRepository,
		f.settingsRepository,
		NewInvoicePDFRenderer(),
		m,
	)
}

// scrapeMetrics renders m's exposition body as a string for substring
// assertions, mirroring httpx's own test helper of the same name (kept
// as its own small copy rather than an exported cross-package helper,
// since this is the only PDF-metrics test file that needs it).
func scrapeMetrics(t *testing.T, m *metrics.Metrics) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))

	return recorder.Body.String()
}

// TestInvoicePDFService_Generate_RecordsSuccessMetric proves a successful
// Generate call increments pdf_generations_total{result="success"} and
// observes the duration histogram — measuring InvoicePDFService.Generate
// itself (data assembly + rendering), not any HTTP-layer span.
func TestInvoicePDFService_Generate_RecordsSuccessMetric(t *testing.T) {
	f := newTestFixture()
	m := metrics.New(nil)
	service := f.pdfServiceWithMetrics(m)

	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.repository.lines[invoiceID] = []*Line{
		{ID: uuid.New(), InvoiceID: invoiceID, Description: "Consulting", Quantity: 1, UnitPrice: 1000, VATRate: 0, VATAmount: 0, Total: 1000},
	}

	if _, _, err := service.Generate(context.Background(), f.organisationID, invoiceID); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	body := scrapeMetrics(t, m)
	if !strings.Contains(body, `go_invoicing_pdf_generations_total{result="success"} 1`) {
		t.Fatalf("expected a success PDF generation sample, got:\n%s", body)
	}
	if !strings.Contains(body, "go_invoicing_pdf_generation_duration_seconds_bucket") {
		t.Fatal("expected the PDF generation duration histogram to have received an observation")
	}
	if strings.Contains(body, invoiceID.String()) || strings.Contains(body, f.organisationID.String()) {
		t.Fatal("expected no invoice or organisation identifier to appear as a metric label")
	}
}

// TestInvoicePDFService_Generate_RecordsErrorMetric proves a failed
// Generate call (invoice not found) increments
// pdf_generations_total{result="error"} — a fixed, bounded enum, never
// the underlying error text.
func TestInvoicePDFService_Generate_RecordsErrorMetric(t *testing.T) {
	f := newTestFixture()
	m := metrics.New(nil)
	service := f.pdfServiceWithMetrics(m)

	if _, _, err := service.Generate(context.Background(), f.organisationID, uuid.New()); err == nil {
		t.Fatal("expected Generate to fail for a non-existent invoice")
	}

	body := scrapeMetrics(t, m)
	if !strings.Contains(body, `go_invoicing_pdf_generations_total{result="error"} 1`) {
		t.Fatalf("expected an error PDF generation sample, got:\n%s", body)
	}
	if strings.Contains(body, `result="success"`) {
		t.Fatal("expected no success sample to have been recorded for a failed generation")
	}
}
