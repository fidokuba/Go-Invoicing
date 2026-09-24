package invoice

import (
	"context"
	"errors"
	"testing"

	admin "go-invoicing/internal/administration"
)

// notVATRegistered switches the fixture's organisation (VAT registered by
// default) to a non-registered one that still has a stored TaxID, so
// tests can prove that TaxID never leaks onto its invoices.
func (f *testFixture) notVATRegistered() {
	taxID := "GB123456789"
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{
		ID: f.organisationID, Name: "Sole Trader", TaxID: &taxID, VATRegistered: false,
	}
}

func TestInvoiceService_Create_NotVATRegisteredRejectsVATRate(t *testing.T) {
	f := newTestFixture()
	f.notVATRegistered()

	zeroRated := CreateInvoiceLineRequest{Description: "Zero", Quantity: 1, UnitPrice: 1000, VATRate: 0}
	vatLine := CreateInvoiceLineRequest{Description: "VAT", Quantity: 1, UnitPrice: 1000, VATRate: 20}

	_, _, _, err := f.service.Create(context.Background(), f.organisationID, validRequest(f.customerID, zeroRated, vatLine))
	if !errors.Is(err, ErrInvoiceLineVATNotPermitted) {
		t.Fatalf("expected ErrInvoiceLineVATNotPermitted, got %v", err)
	}

	if f.tx.committed {
		t.Error("expected nothing to be committed for a rejected invoice")
	}
}

func TestInvoiceService_Create_NotVATRegisteredCapturesStatus(t *testing.T) {
	f := newTestFixture()
	f.notVATRegistered()

	line := CreateInvoiceLineRequest{Description: "Consulting", Quantity: 2, UnitPrice: 1000, VATRate: 0}

	inv, _, _, err := f.service.Create(context.Background(), f.organisationID, validRequest(f.customerID, line))
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if inv.VATRegistered {
		t.Error("expected the invoice to be captured as not VAT registered")
	}

	if inv.Subtotal != 2000 || inv.VATTotal != 0 || inv.Total != 2000 {
		t.Errorf("expected subtotal=2000 vatTotal=0 total=2000, got subtotal=%d vatTotal=%d total=%d",
			inv.Subtotal, inv.VATTotal, inv.Total)
	}

	if f.repository.invoices[inv.ID].VATRegistered {
		t.Error("expected the persisted invoice to be not VAT registered")
	}
}

func TestInvoiceService_Create_VATRegisteredCapturesStatus(t *testing.T) {
	f := newTestFixture()

	inv, _, _, err := f.service.Create(context.Background(), f.organisationID, validRequest(f.customerID))
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if !inv.VATRegistered {
		t.Error("expected the invoice to be captured as VAT registered")
	}

	if inv.VATTotal != 200 {
		t.Errorf("expected vatTotal=200, got %d", inv.VATTotal)
	}
}

// TestInvoiceService_Create_StatusNotAffectedByLaterChange: the status is
// captured at creation — a later change to the organisation does not
// rewrite an existing invoice.
func TestInvoiceService_Create_StatusNotAffectedByLaterChange(t *testing.T) {
	f := newTestFixture()

	inv, _, _, err := f.service.Create(context.Background(), f.organisationID, validRequest(f.customerID))
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	f.notVATRegistered()

	got, _, _, _, err := f.service.GetByID(context.Background(), f.organisationID, inv.ID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if !got.VATRegistered {
		t.Error("expected the invoice to remain VAT registered after the organisation changed")
	}
}

func TestInvoiceService_Send_NotVATRegisteredOmitsSellerTaxID(t *testing.T) {
	f := newTestFixture()
	f.notVATRegistered()

	line := CreateInvoiceLineRequest{Description: "Consulting", Quantity: 1, UnitPrice: 1000, VATRate: 0}
	inv, _, _, err := f.service.Create(context.Background(), f.organisationID, validRequest(f.customerID, line))
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	sent, err := f.service.Send(context.Background(), f.organisationID, inv.ID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if sent.SellerTaxID != nil {
		t.Errorf("expected no seller Tax ID in the snapshot, got %q", *sent.SellerTaxID)
	}
}

func TestInvoicePDFService_Draft_NotVATRegisteredHidesVAT(t *testing.T) {
	f := newTestFixture()
	f.notVATRegistered()

	line := CreateInvoiceLineRequest{Description: "Consulting", Quantity: 1, UnitPrice: 1000, VATRate: 0}
	inv, _, _, err := f.service.Create(context.Background(), f.organisationID, validRequest(f.customerID, line))
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, inv.ID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.VATRegistered {
		t.Error("expected PDF data to be marked not VAT registered")
	}

	if data.Seller.TaxID != "" {
		t.Errorf("expected no seller Tax ID on the PDF, got %q", data.Seller.TaxID)
	}
}

func TestInvoicePDFService_Draft_VATRegisteredShowsVAT(t *testing.T) {
	f := newTestFixture()
	taxID := "GB123456789"
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{
		ID: f.organisationID, Name: "Acme Ltd", TaxID: &taxID, VATRegistered: true,
	}

	inv, _, _, err := f.service.Create(context.Background(), f.organisationID, validRequest(f.customerID))
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, inv.ID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if !data.VATRegistered {
		t.Error("expected PDF data to be marked VAT registered")
	}

	if data.Seller.TaxID != taxID {
		t.Errorf("expected seller Tax ID %q, got %q", taxID, data.Seller.TaxID)
	}
}
