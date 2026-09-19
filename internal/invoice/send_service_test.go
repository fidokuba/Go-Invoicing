package invoice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
)

func TestInvoiceService_Send_Success(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if inv.Status != InvoiceStatusSent {
		t.Errorf("expected status %q, got %q", InvoiceStatusSent, inv.Status)
	}

	if inv.SentAt == nil {
		t.Fatal("expected SentAt to be populated")
	}

	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusSent {
		t.Errorf("expected persisted status %q, got %q", InvoiceStatusSent, persisted.Status)
	}

	if persisted.SentAt == nil {
		t.Error("expected persisted SentAt to be populated")
	}

	if !f.tx.committed {
		t.Error("expected the transaction to be committed")
	}

	if f.tx.rolledBack {
		t.Error("expected the transaction not to be rolled back")
	}
}

// --- Snapshot capture (Milestone 7 Part 2) ---

func TestInvoiceService_Send_CapturesAllAvailableSellerFields(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	email, phone, website := "seller@acme.test", "+44 20 7946 0958", "https://acme.test"
	address, city, state, postalCode, country := "1 Acme Way", "London", "Greater London", "E1 6AN", "GB"
	taxID := "GB123456789"
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{
		ID: f.organisationID, Name: "Acme Ltd",
		Email: &email, Phone: &phone, Website: &website,
		Address: &address, City: &city, State: &state, PostalCode: &postalCode, Country: &country,
		TaxID: &taxID,
	}

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	checks := map[string]*string{
		"SellerName": inv.SellerName, "SellerEmail": inv.SellerEmail, "SellerPhone": inv.SellerPhone,
		"SellerWebsite": inv.SellerWebsite, "SellerAddress": inv.SellerAddress, "SellerCity": inv.SellerCity,
		"SellerState": inv.SellerState, "SellerPostalCode": inv.SellerPostalCode,
		"SellerCountry": inv.SellerCountry, "SellerTaxID": inv.SellerTaxID,
	}
	for field, got := range checks {
		if got == nil || *got == "" {
			t.Errorf("expected %s to be captured, got %v", field, got)
		}
	}

	if *inv.SellerName != "Acme Ltd" {
		t.Errorf("expected SellerName %q, got %q", "Acme Ltd", *inv.SellerName)
	}
}

func TestInvoiceService_Send_CapturesCustomerIdentity(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	companyName, email, phone, taxID := "Bob's Bakery Ltd", "bob@bakery.test", "+44 161 496 0000", "GB987654321"
	f.customerRepository.customers[f.customerID] = customer.Customer{
		ID: f.customerID, OrganisationID: f.organisationID, Name: "Bob's Bakery",
		CompanyName: &companyName, Email: &email, Phone: &phone, TaxID: &taxID,
	}

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if inv.CustomerName == nil || *inv.CustomerName != "Bob's Bakery" {
		t.Errorf("expected CustomerName %q, got %v", "Bob's Bakery", inv.CustomerName)
	}
	if inv.CustomerCompanyName == nil || *inv.CustomerCompanyName != companyName {
		t.Errorf("expected CustomerCompanyName %q, got %v", companyName, inv.CustomerCompanyName)
	}
	if inv.CustomerEmail == nil || *inv.CustomerEmail != email {
		t.Errorf("expected CustomerEmail %q, got %v", email, inv.CustomerEmail)
	}
	if inv.CustomerTaxID == nil || *inv.CustomerTaxID != taxID {
		t.Errorf("expected CustomerTaxID %q, got %v", taxID, inv.CustomerTaxID)
	}
}

func TestInvoiceService_Send_CapturesBillingAddress(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	f.addressRepository.add(f.customerID, customer.Address{
		Street: "2 Bakery Street", City: "Manchester", State: "Greater Manchester",
		PostalCode: "M1 1AE", Country: "GB",
	})

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if inv.CustomerAddress == nil || *inv.CustomerAddress != "2 Bakery Street" {
		t.Errorf("expected CustomerAddress %q, got %v", "2 Bakery Street", inv.CustomerAddress)
	}
	if inv.CustomerCity == nil || *inv.CustomerCity != "Manchester" {
		t.Errorf("expected CustomerCity %q, got %v", "Manchester", inv.CustomerCity)
	}
	if inv.CustomerPostalCode == nil || *inv.CustomerPostalCode != "M1 1AE" {
		t.Errorf("expected CustomerPostalCode %q, got %v", "M1 1AE", inv.CustomerPostalCode)
	}
}

// TestInvoiceService_Send_WithoutBillingAddressSucceeds proves Milestone
// 7 Part 1's design decision holds through Send: no billing address is
// acceptable, not a blocker.
func TestInvoiceService_Send_WithoutBillingAddressSucceeds(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("expected send to succeed without a billing address, got %v", err)
	}

	if inv.CustomerAddress != nil || inv.CustomerCity != nil || inv.CustomerPostalCode != nil || inv.CustomerCountry != nil || inv.CustomerState != nil {
		t.Error("expected every customer-address snapshot field to remain nil")
	}
}

func TestInvoiceService_Send_CurrencyCapturedFromSettings(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.settingsRepository.settings[f.organisationID] = admin.Settings{
		OrganisationID: f.organisationID, Currency: "eur", // lowercase — must be normalized
	}

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if inv.Currency == nil || *inv.Currency != "EUR" {
		t.Errorf("expected Currency %q, got %v", "EUR", inv.Currency)
	}
}

func TestInvoiceService_Send_MissingSellerNamePreventsSend(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: ""}

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceSnapshotSellerNameRequired) {
		t.Fatalf("expected ErrInvoiceSnapshotSellerNameRequired, got %v", err)
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q after a rejected snapshot, got %q", InvoiceStatusDraft, persisted.Status)
	}
}

func TestInvoiceService_Send_MissingCustomerNamePreventsSend(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.customerRepository.customers[f.customerID] = customer.Customer{ID: f.customerID, OrganisationID: f.organisationID, Name: ""}

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceSnapshotCustomerNameRequired) {
		t.Fatalf("expected ErrInvoiceSnapshotCustomerNameRequired, got %v", err)
	}
}

func TestInvoiceService_Send_MissingSettingsPreventsSend(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.settingsRepository = newFakeSettingsRepository() // no settings row at all
	f.service.settingsRepository = f.settingsRepository

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceSettingsNotFound) {
		t.Fatalf("expected ErrInvoiceSettingsNotFound, got %v", err)
	}
}

// TestInvoiceService_Send_OrganisationLookupFailureRollsBack proves an
// unexpected failure loading a *related* record (not the invoice itself)
// still rolls the whole transaction back and never partially persists
// anything.
func TestInvoiceService_Send_OrganisationLookupFailureRollsBack(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	delete(f.organisationRepository.organisations, f.organisationID) // organisation "disappears"

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceSnapshotDataUnavailable) {
		t.Fatalf("expected ErrInvoiceSnapshotDataUnavailable, got %v", err)
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}
}

// TestInvoiceService_Send_RepeatedSendNeverReloadsSnapshotSources is the
// explicit proof for Milestone 7 Part 2's "the second Send must NOT
// recapture current Organisation/Customer/Address/Settings data"
// requirement: after a successful first Send, every one of those
// repositories' read-call counters is captured, a second Send is
// attempted and rejected, and the counters must be unchanged — the
// snapshot-building code must never even run for a non-Draft invoice.
func TestInvoiceService_Send_RepeatedSendNeverReloadsSnapshotSources(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	if _, err := f.service.Send(context.Background(), f.organisationID, invoiceID); err != nil {
		t.Fatalf("first send: %v", err)
	}

	orgCallsBefore := f.organisationRepository.getByIDCallCount
	addressCallsBefore := f.addressRepository.getCallCount
	settingsCallsBefore := f.settingsRepository.getByOrganisationIDCallCount
	sentAtBefore := f.repository.invoices[invoiceID].SentAt
	sellerNameBefore := f.repository.invoices[invoiceID].SellerName

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent on the repeat send, got %v", err)
	}

	if f.organisationRepository.getByIDCallCount != orgCallsBefore {
		t.Errorf("expected no additional organisation lookups on a repeat send, before=%d after=%d", orgCallsBefore, f.organisationRepository.getByIDCallCount)
	}
	if f.addressRepository.getCallCount != addressCallsBefore {
		t.Errorf("expected no additional billing-address lookups on a repeat send, before=%d after=%d", addressCallsBefore, f.addressRepository.getCallCount)
	}
	if f.settingsRepository.getByOrganisationIDCallCount != settingsCallsBefore {
		t.Errorf("expected no additional settings lookups on a repeat send, before=%d after=%d", settingsCallsBefore, f.settingsRepository.getByOrganisationIDCallCount)
	}

	sentAtAfter := f.repository.invoices[invoiceID].SentAt
	if !sentAtAfter.Equal(*sentAtBefore) {
		t.Errorf("expected SentAt to remain %v, got %v", *sentAtBefore, *sentAtAfter)
	}

	sellerNameAfter := f.repository.invoices[invoiceID].SellerName
	if *sellerNameAfter != *sellerNameBefore {
		t.Errorf("expected SellerName to remain %q, got %q", *sellerNameBefore, *sellerNameAfter)
	}
}

func TestInvoiceService_Send_NotFound(t *testing.T) {
	f := newTestFixture()

	_, err := f.service.Send(context.Background(), f.organisationID, uuid.New())
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound, got %v", err)
	}
}

// TestInvoiceService_Send_WrongOrganisation proves the existing
// organisation-scoped GetForUpdate behaviour (Milestone 4) is what Send
// relies on for tenant isolation — a Draft invoice under a different
// organisation is treated exactly like one that doesn't exist.
func TestInvoiceService_Send_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	_, err := f.service.Send(context.Background(), uuid.New(), invoiceID)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-organisation send, got %v", err)
	}

	// Must not have been transitioned via the correct organisation's view
	// either — the wrong-organisation attempt must not have mutated it.
	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusDraft, persisted.Status)
	}
}

func TestInvoiceService_Send_AlreadySent(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}
}

func TestInvoiceService_Send_Paid(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusPaid)

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusPaid {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusPaid, persisted.Status)
	}
}

// TestInvoiceService_Send_RejectedTransitionPerformsNoLifecycleUpdate
// proves a rejected Send never even calls the repository's MarkSent —
// the in-memory Invoice.MarkSent guard fails first, so there is no
// lifecycle write for the transaction to roll back in the first place.
func TestInvoiceService_Send_RejectedTransitionPerformsNoLifecycleUpdate(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	beforeSentAt := f.repository.invoices[invoiceID].SentAt

	if _, err := f.service.Send(context.Background(), f.organisationID, invoiceID); !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	afterSentAt := f.repository.invoices[invoiceID].SentAt
	if beforeSentAt != nil || afterSentAt != nil {
		t.Errorf("expected SentAt to remain nil throughout, got before=%v after=%v", beforeSentAt, afterSentAt)
	}
}

func TestInvoiceService_Send_BeginTransactionErrorPropagates(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.service.txBeginner = &fakeTxBeginner{beginErr: errors.New("pool exhausted")}

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}

	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusDraft, persisted.Status)
	}
}

func TestInvoiceService_Send_GetForUpdateErrorPropagates(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.repository.getForUpdateErr = errors.New("connection reset by peer")

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestInvoiceService_Send_RollsBackOnMarkSentFailure(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.repository.markSentWithSnapshotErr = errors.New("connection reset by peer")

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestInvoiceService_Send_CommitFailureDoesNotReportSuccess(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.tx.commitErr = errors.New("connection reset by peer")

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error when commit fails")
	}

	if inv != nil {
		t.Errorf("expected a nil result on commit failure, got %+v", inv)
	}
}
