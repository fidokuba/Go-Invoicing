package invoice

import (
	"context"
	"errors"
	"testing"
)

// TestInvoiceService_Create_UsesLiveSettingsCurrency proves a freshly
// created (always-Draft) invoice's response currency comes from the
// organisation's current Settings.Currency — there is no snapshot yet to
// protect.
func TestInvoiceService_Create_UsesLiveSettingsCurrency(t *testing.T) {
	f := newTestFixture()

	request := validCreateInvoiceRequest(f.customerID.String())
	_, _, currency, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if currency != "GBP" {
		t.Errorf("expected currency %q (the fixture's default Settings.Currency), got %q", "GBP", currency)
	}
}

// TestInvoiceService_GetByID_DraftUsesCurrentLiveSettingsCurrency proves
// GetByID re-resolves a Draft invoice's currency from live settings on
// every read, not just at creation time — changing the organisation's
// currency between creating and reading a still-Draft invoice must be
// reflected, exactly as every other live field on a Draft invoice is.
func TestInvoiceService_GetByID_DraftUsesCurrentLiveSettingsCurrency(t *testing.T) {
	f := newTestFixture()

	request := validCreateInvoiceRequest(f.customerID.String())
	created, _, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	_, _, _, currency, err := f.service.GetByID(context.Background(), f.organisationID, created.ID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}
	if currency != "GBP" {
		t.Errorf("expected currency %q, got %q", "GBP", currency)
	}

	// Change the organisation's live currency — still Draft, so this
	// must be picked up.
	settings := f.settingsRepository.settings[f.organisationID]
	settings.Currency = "EUR"
	f.settingsRepository.settings[f.organisationID] = settings

	_, _, _, currency, err = f.service.GetByID(context.Background(), f.organisationID, created.ID)
	if err != nil {
		t.Fatalf("get invoice after settings change: %v", err)
	}
	if currency != "EUR" {
		t.Errorf("expected currency to follow the updated live Settings.Currency %q, got %q", "EUR", currency)
	}
}

// TestInvoiceService_GetByID_IssuedInvoiceUsesSnapshotCurrency proves an
// issued (Sent) invoice's currency comes from its immutable Currency
// snapshot, captured once at Send, matching the organisation's currency
// at that moment.
func TestInvoiceService_GetByID_IssuedInvoiceUsesSnapshotCurrency(t *testing.T) {
	f := newTestFixture()

	request := validCreateInvoiceRequest(f.customerID.String())
	created, _, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if _, err := f.service.Send(context.Background(), f.organisationID, created.ID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	_, _, _, currency, err := f.service.GetByID(context.Background(), f.organisationID, created.ID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}
	if currency != "GBP" {
		t.Errorf("expected snapshot currency %q, got %q", "GBP", currency)
	}
}

// TestInvoiceService_SentInvoiceCurrency_UnaffectedByLaterSettingsChange
// is the core Milestone 8 Part 2 historical-accuracy proof: once an
// invoice is Sent, changing the organisation's live Settings.Currency
// must NEVER change what that invoice's response currency claims — the
// same historical-accuracy rule Milestone 7 Part 2 already established
// for the seller/customer snapshot fields, now also applied to currency.
func TestInvoiceService_SentInvoiceCurrency_UnaffectedByLaterSettingsChange(t *testing.T) {
	f := newTestFixture()

	request := validCreateInvoiceRequest(f.customerID.String())
	created, _, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if _, err := f.service.Send(context.Background(), f.organisationID, created.ID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	// Change the organisation's live currency after the invoice was sent.
	settings := f.settingsRepository.settings[f.organisationID]
	settings.Currency = "USD"
	f.settingsRepository.settings[f.organisationID] = settings

	_, _, _, currency, err := f.service.GetByID(context.Background(), f.organisationID, created.ID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if currency != "GBP" {
		t.Errorf("expected the sent invoice's currency to remain the snapshotted %q, got %q (must not silently follow the live organisation currency)", "GBP", currency)
	}
}

// TestInvoiceService_GetByID_LegacyIssuedInvoiceMissingCurrency_DoesNotSubstituteLiveCurrency
// proves the deliberate handling chosen for an issued invoice with no
// currency snapshot at all (only possible for an invoice sent before
// Milestone 7 Part 2 introduced that column): GetByID must fail with
// ErrInvoiceCurrencyUnavailable rather than silently falling back to the
// organisation's current live currency, which could misrepresent that
// invoice's actual historical currency.
func TestInvoiceService_GetByID_LegacyIssuedInvoiceMissingCurrency_DoesNotSubstituteLiveCurrency(t *testing.T) {
	f := newTestFixture()

	// addInvoice sets Currency by default (see its own comment) — clear
	// it here to simulate a genuine pre-Milestone-7-Part-2 legacy row.
	invoiceID := f.addInvoice(1000, InvoiceStatusSent)
	inv := f.repository.invoices[invoiceID]
	inv.Currency = nil
	f.repository.invoices[invoiceID] = inv

	_, _, _, _, err := f.service.GetByID(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceCurrencyUnavailable) {
		t.Fatalf("expected ErrInvoiceCurrencyUnavailable, got %v", err)
	}
}

// TestInvoiceService_GetByID_DraftCurrencyUnavailable_WhenSettingsMissing
// proves the Draft-side counterpart: if the organisation has no settings
// row at all, GetByID must fail rather than hardcode a default currency
// like GBP.
func TestInvoiceService_GetByID_DraftCurrencyUnavailable_WhenSettingsMissing(t *testing.T) {
	f := newTestFixture()

	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	delete(f.settingsRepository.settings, f.organisationID)

	_, _, _, _, err := f.service.GetByID(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceCurrencyUnavailable) {
		t.Fatalf("expected ErrInvoiceCurrencyUnavailable, got %v", err)
	}
}

// validCreateInvoiceRequest returns a minimal, structurally valid
// CreateInvoiceRequest for customerID — shared by every test in this
// file so each one only needs to state what it's actually testing.
func validCreateInvoiceRequest(customerID string) CreateInvoiceRequest {
	return CreateInvoiceRequest{
		CustomerID: customerID,
		IssueDate:  "2026-06-01",
		DueDate:    "2026-06-30",
		Lines: []CreateInvoiceLineRequest{
			{Description: "Consulting", Quantity: 1, UnitPrice: 10000, VATRate: 20},
		},
	}
}
