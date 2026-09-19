package invoice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
)

// --- Draft: uses live data ---

func TestInvoicePDFService_Draft_UsesCurrentOrganisation(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: "Current Org Name"}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Seller.Name != "Current Org Name" {
		t.Errorf("expected seller name %q, got %q", "Current Org Name", data.Seller.Name)
	}
}

func TestInvoicePDFService_Draft_UsesCurrentCustomer(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.customerRepository.customers[f.customerID] = customer.Customer{ID: f.customerID, OrganisationID: f.organisationID, Name: "Current Customer Name"}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Customer.DisplayName != "Current Customer Name" {
		t.Errorf("expected customer display name %q, got %q", "Current Customer Name", data.Customer.DisplayName)
	}
}

func TestInvoicePDFService_Draft_UsesCurrentBillingAddress(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.addressRepository.add(f.customerID, customer.Address{
		Street: "2 Bakery Street", City: "Manchester", PostalCode: "M1 1AE", Country: "GB",
	})

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	joined := strings.Join(data.Customer.AddressLines, " | ")
	if !strings.Contains(joined, "2 Bakery Street") {
		t.Errorf("expected billing address to be used, got %v", data.Customer.AddressLines)
	}
}

func TestInvoicePDFService_Draft_UsesCurrentSettingsCurrency(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.settingsRepository.settings[f.organisationID] = admin.Settings{OrganisationID: f.organisationID, Currency: "EUR"}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if !strings.Contains(data.Total, "€") {
		t.Errorf("expected EUR currency to be used in formatted total, got %q", data.Total)
	}
}

func TestInvoicePDFService_Draft_ChangingLiveDataChangesSubsequentPDF(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: "Before"}

	first, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data (first): %v", err)
	}
	if first.Seller.Name != "Before" {
		t.Fatalf("expected seller name %q, got %q", "Before", first.Seller.Name)
	}

	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: "After"}

	second, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data (second): %v", err)
	}
	if second.Seller.Name != "After" {
		t.Errorf("expected a Draft PDF built after a live change to reflect it — expected %q, got %q", "After", second.Seller.Name)
	}
}

func TestInvoicePDFService_Draft_MissingBillingAddressSucceeds(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	// No billing address added.

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("expected build to succeed without a billing address, got %v", err)
	}

	if len(data.Customer.AddressLines) != 0 {
		t.Errorf("expected no customer address lines, got %v", data.Customer.AddressLines)
	}
}

func TestInvoicePDFService_Draft_MissingOrganisationNameFails(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: ""}

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoicePDFSellerNameMissing) {
		t.Fatalf("expected ErrInvoicePDFSellerNameMissing, got %v", err)
	}
}

func TestInvoicePDFService_Draft_MissingCustomerNameFails(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.customerRepository.customers[f.customerID] = customer.Customer{ID: f.customerID, OrganisationID: f.organisationID, Name: ""}

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoicePDFCustomerNameMissing) {
		t.Fatalf("expected ErrInvoicePDFCustomerNameMissing, got %v", err)
	}
}

func TestInvoicePDFService_Draft_MissingSettingsFails(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.settingsRepository = newFakeSettingsRepository() // no settings row at all
	f.service.settingsRepository = f.settingsRepository

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoicePDFCurrencyMissing) {
		t.Fatalf("expected ErrInvoicePDFCurrencyMissing, got %v", err)
	}
}

// --- Issued: uses immutable snapshot only ---

func TestInvoicePDFService_Issued_UsesSellerSnapshot(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Seller.Name != "Snapshot Seller Ltd" {
		t.Errorf("expected snapshot seller name, got %q", data.Seller.Name)
	}
}

func TestInvoicePDFService_Issued_UsesCustomerSnapshot(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Customer.DisplayName != "Snapshot Customer" {
		t.Errorf("expected snapshot customer name, got %q", data.Customer.DisplayName)
	}
}

func TestInvoicePDFService_Issued_UsesCurrencySnapshot(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))
	// Live settings say EUR, but the snapshot says GBP — the snapshot
	// must win for an issued invoice.
	f.settingsRepository.settings[f.organisationID] = admin.Settings{OrganisationID: f.organisationID, Currency: "EUR"}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if !strings.Contains(data.Total, "£") {
		t.Errorf("expected the GBP snapshot currency to be used, got %q", data.Total)
	}
}

// TestInvoicePDFService_Issued_DoesNotCallLiveRepositories is the
// central proof for Milestone 7 Part 3 section 5: none of the live
// organisation/customer/address/settings repositories are touched at
// all when building an issued invoice's PDF data.
func TestInvoicePDFService_Issued_DoesNotCallLiveRepositories(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))

	orgCallsBefore := f.organisationRepository.getByIDCallCount
	custCallsBefore := f.customerRepository.getByIDCallCount
	addressCallsBefore := f.addressRepository.getCallCount
	settingsCallsBefore := f.settingsRepository.getByOrganisationIDCallCount

	if _, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID); err != nil {
		t.Fatalf("build data: %v", err)
	}

	if f.organisationRepository.getByIDCallCount != orgCallsBefore {
		t.Errorf("expected no organisation repository calls, before=%d after=%d", orgCallsBefore, f.organisationRepository.getByIDCallCount)
	}
	if f.customerRepository.getByIDCallCount != custCallsBefore {
		t.Errorf("expected no customer repository calls, before=%d after=%d", custCallsBefore, f.customerRepository.getByIDCallCount)
	}
	if f.addressRepository.getCallCount != addressCallsBefore {
		t.Errorf("expected no address repository calls, before=%d after=%d", addressCallsBefore, f.addressRepository.getCallCount)
	}
	if f.settingsRepository.getByOrganisationIDCallCount != settingsCallsBefore {
		t.Errorf("expected no settings repository calls, before=%d after=%d", settingsCallsBefore, f.settingsRepository.getByOrganisationIDCallCount)
	}
}

func TestInvoicePDFService_Issued_ChangingLiveDataDoesNotAffectPDF(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))

	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: "Changed Live Name"}
	f.customerRepository.customers[f.customerID] = customer.Customer{ID: f.customerID, OrganisationID: f.organisationID, Name: "Changed Live Customer"}
	f.settingsRepository.settings[f.organisationID] = admin.Settings{OrganisationID: f.organisationID, Currency: "USD"}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Seller.Name != "Snapshot Seller Ltd" {
		t.Errorf("expected the snapshot seller name to remain unaffected, got %q", data.Seller.Name)
	}
	if data.Customer.DisplayName != "Snapshot Customer" {
		t.Errorf("expected the snapshot customer name to remain unaffected, got %q", data.Customer.DisplayName)
	}
	if !strings.Contains(data.Total, "£") {
		t.Errorf("expected the snapshot GBP currency to remain unaffected, got %q", data.Total)
	}
}

func TestInvoicePDFService_Issued_MissingSnapshotSellerNameFails(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))
	inv := f.repository.invoices[invoiceID]
	inv.SellerName = nil
	f.repository.invoices[invoiceID] = inv

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoicePDFSellerNameMissing) {
		t.Fatalf("expected ErrInvoicePDFSellerNameMissing, got %v", err)
	}
}

func TestInvoicePDFService_Issued_MissingSnapshotCustomerNameFails(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))
	inv := f.repository.invoices[invoiceID]
	inv.CustomerName = nil
	f.repository.invoices[invoiceID] = inv

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoicePDFCustomerNameMissing) {
		t.Fatalf("expected ErrInvoicePDFCustomerNameMissing, got %v", err)
	}
}

func TestInvoicePDFService_Issued_MissingSnapshotCurrencyFails(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))
	inv := f.repository.invoices[invoiceID]
	inv.Currency = nil
	f.repository.invoices[invoiceID] = inv

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoicePDFCurrencyMissing) {
		t.Fatalf("expected ErrInvoicePDFCurrencyMissing, got %v", err)
	}
}

// TestInvoicePDFService_Issued_EffectivelyOverdueStillUsesSnapshot proves
// a persisted-Sent-but-effectively-Overdue invoice still uses the
// snapshot, not live data, exactly like a plain Sent invoice — Overdue
// is a display concept only (Milestone 7 Part 3 section 5).
func TestInvoicePDFService_Issued_EffectivelyOverdueStillUsesSnapshot(t *testing.T) {
	f := newTestFixture()
	pastDueDate := time.Now().UTC().AddDate(0, 0, -30)
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, pastDueDate)
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: "Changed Live Name"}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.StatusLabel != "OVERDUE" {
		t.Errorf("expected StatusLabel %q, got %q", "OVERDUE", data.StatusLabel)
	}
	if data.Seller.Name != "Snapshot Seller Ltd" {
		t.Errorf("expected snapshot seller name despite Overdue display status, got %q", data.Seller.Name)
	}
}

func TestInvoicePDFService_Issued_PaidStillUsesSnapshot(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusPaid, time.Now().UTC().AddDate(0, 0, 30))
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: "Changed Live Name"}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.StatusLabel != "PAID" {
		t.Errorf("expected StatusLabel %q, got %q", "PAID", data.StatusLabel)
	}
	if data.Seller.Name != "Snapshot Seller Ltd" {
		t.Errorf("expected snapshot seller name for a Paid invoice, got %q", data.Seller.Name)
	}
}

// --- Lines, payments, tenant isolation, repository failures ---

func TestInvoicePDFService_CorrectLines(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1200, InvoiceStatusDraft)
	f.repository.lines[invoiceID] = []*Line{
		{ID: uuid.New(), InvoiceID: invoiceID, Description: "Consulting", Quantity: 1, UnitPrice: 1000, VATRate: 20, VATAmount: 200, Total: 1200},
	}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if len(data.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(data.Lines))
	}
	line := data.Lines[0]
	if line.Description != "Consulting" || line.Quantity != "1" || line.VATRate != "20%" {
		t.Errorf("unexpected line contents: %+v", line)
	}
}

func TestInvoicePDFService_CorrectPaymentTotals(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addIssuedInvoiceWithSnapshot(1000, InvoiceStatusSent, time.Now().UTC().AddDate(0, 0, 30))
	f.paymentRepository.payments[invoiceID] = []*Payment{{ID: uuid.New(), InvoiceID: invoiceID, Amount: 400}}

	data, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if !data.ShowPaymentSummary {
		t.Fatal("expected ShowPaymentSummary for a partial payment")
	}
	if data.AmountPaid == "" || data.AmountOutstanding == "" {
		t.Error("expected AmountPaid and AmountOutstanding to be populated")
	}
}

func TestInvoicePDFService_TenantIsolation(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)

	_, err := f.pdfService().BuildData(context.Background(), uuid.New(), invoiceID)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-tenant lookup, got %v", err)
	}
}

func TestInvoicePDFService_InvoiceRepositoryFailurePropagates(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.repository.getByIDErr = errors.New("connection reset by peer")

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestInvoicePDFService_PaymentRepositoryFailurePropagates(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.paymentRepository.getTotalPaidErr = errors.New("connection reset by peer")

	_, err := f.pdfService().BuildData(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestInvoicePDFService_Generate_ProducesValidPDFBytes(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.repository.lines[invoiceID] = []*Line{
		{ID: uuid.New(), InvoiceID: invoiceID, Description: "Consulting", Quantity: 1, UnitPrice: 1000, VATRate: 0, VATAmount: 0, Total: 1000},
	}

	pdfBytes, invoiceNumber, err := f.pdfService().Generate(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	requireValidPDFHeader(t, pdfBytes)

	if invoiceNumber == "" {
		t.Error("expected a non-empty invoice number")
	}
}
