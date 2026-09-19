package invoice

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestInvoiceService_List_ValidatesStatusFilter proves an unrecognised
// ?status= value is rejected before ever reaching the repository.
func TestInvoiceService_List_ValidatesStatusFilter(t *testing.T) {
	f := newTestFixture()

	_, _, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{Status: "bogus", Limit: 50}, time.Now())
	if !errors.Is(err, ErrInvoiceListStatusInvalid) {
		t.Fatalf("expected ErrInvoiceListStatusInvalid, got %v", err)
	}
}

// TestInvoiceService_List_ValidatesDateRangeOrdering proves a From
// strictly after its matching To is rejected for both the issue-date and
// due-date pairs, before ever reaching the repository.
func TestInvoiceService_List_ValidatesDateRangeOrdering(t *testing.T) {
	f := newTestFixture()

	late := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	early := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	t.Run("issue date range reversed", func(t *testing.T) {
		_, _, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{
			IssueDateFrom: &late, IssueDateTo: &early, Limit: 50,
		}, time.Now())
		if !errors.Is(err, ErrInvoiceListIssueDateRangeInvalid) {
			t.Fatalf("expected ErrInvoiceListIssueDateRangeInvalid, got %v", err)
		}
	})

	t.Run("due date range reversed", func(t *testing.T) {
		_, _, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{
			DueDateFrom: &late, DueDateTo: &early, Limit: 50,
		}, time.Now())
		if !errors.Is(err, ErrInvoiceListDueDateRangeInvalid) {
			t.Fatalf("expected ErrInvoiceListDueDateRangeInvalid, got %v", err)
		}
	})
}

// TestInvoiceService_List_DraftItemsUseLiveSettingsCurrency proves a
// Draft row in a list result shows the organisation's current
// Settings.Currency, exactly like GetByID.
func TestInvoiceService_List_DraftItemsUseLiveSettingsCurrency(t *testing.T) {
	f := newTestFixture()

	request := validCreateInvoiceRequest(f.customerID.String())
	if _, _, _, err := f.service.Create(context.Background(), f.organisationID, request); err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	items, total, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{Limit: 50}, time.Now())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected exactly 1 invoice, got total=%d items=%+v", total, items)
	}
	if items[0].Currency != "GBP" {
		t.Errorf("expected currency %q, got %q", "GBP", items[0].Currency)
	}
}

// TestInvoiceService_List_IssuedItemUsesSnapshotCurrency_UnaffectedByLaterSettingsChange
// mirrors the GetByID currency-history tests for List specifically:
// an issued invoice's list-row currency must come from its snapshot,
// and must not move when the organisation's live currency changes later.
func TestInvoiceService_List_IssuedItemUsesSnapshotCurrency_UnaffectedByLaterSettingsChange(t *testing.T) {
	f := newTestFixture()

	request := validCreateInvoiceRequest(f.customerID.String())
	created, _, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	if _, err := f.service.Send(context.Background(), f.organisationID, created.ID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	settings := f.settingsRepository.settings[f.organisationID]
	settings.Currency = "USD"
	f.settingsRepository.settings[f.organisationID] = settings

	items, _, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{Limit: 50}, time.Now())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].Currency != "GBP" {
		t.Fatalf("expected the sent invoice's list-row currency to remain the snapshotted GBP, got %+v", items)
	}
}

// TestInvoiceService_List_LegacyMissingCurrencyFailsWholeRequest proves
// the Milestone 8 Part 3 section 10 decision: one malformed legacy row
// (an issued invoice with no currency snapshot) fails the whole List
// request rather than silently omitting that row or fabricating a
// currency for it.
func TestInvoiceService_List_LegacyMissingCurrencyFailsWholeRequest(t *testing.T) {
	f := newTestFixture()

	// A normal, healthy issued invoice...
	invoiceID := f.addInvoice(1000, InvoiceStatusSent)
	// ...and a legacy one with no currency snapshot at all, simulating a
	// pre-Milestone-7-Part-2 row.
	legacyID := f.addInvoice(2000, InvoiceStatusSent)
	legacy := f.repository.invoices[legacyID]
	legacy.Currency = nil
	f.repository.invoices[legacyID] = legacy

	_, _, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{Limit: 50}, time.Now())
	if !errors.Is(err, ErrInvoiceCurrencyUnavailable) {
		t.Fatalf("expected ErrInvoiceCurrencyUnavailable for the whole request, got %v", err)
	}

	_ = invoiceID // referenced only to document the healthy sibling row exists
}

// TestInvoiceService_List_NoNPlusOneQueries is the Milestone 8 Part 3
// section 30 regression test: listing several invoices — a mix of Draft
// and issued — must call GetByOrganisationID (settings) at most once and
// GetTotalPaidByInvoiceIDs (the batched payment-total query) exactly
// once, never once per invoice.
func TestInvoiceService_List_NoNPlusOneQueries(t *testing.T) {
	f := newTestFixture()

	// Three Draft invoices (all would need settings) plus two issued
	// invoices (snapshot currency, no settings needed) — five rows total,
	// enough that an N+1 pattern would be obviously distinguishable (5
	// calls) from the intended batched pattern (1 call).
	for i := 0; i < 3; i++ {
		request := validCreateInvoiceRequest(f.customerID.String())
		if _, _, _, err := f.service.Create(context.Background(), f.organisationID, request); err != nil {
			t.Fatalf("create draft invoice %d: %v", i, err)
		}
	}
	f.addInvoice(1000, InvoiceStatusSent)
	f.addInvoice(2000, InvoiceStatusPaid)

	f.settingsRepository.getByOrganisationIDCallCount = 0
	f.paymentRepository.getTotalPaidByInvoiceIDsCallCount = 0

	items, total, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{Limit: 50}, time.Now())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 5 || len(items) != 5 {
		t.Fatalf("expected 5 invoices, got total=%d items=%d", total, len(items))
	}

	if f.settingsRepository.getByOrganisationIDCallCount != 1 {
		t.Errorf("expected exactly 1 settings lookup for the whole page, got %d", f.settingsRepository.getByOrganisationIDCallCount)
	}
	if f.paymentRepository.getTotalPaidByInvoiceIDsCallCount != 1 {
		t.Errorf("expected exactly 1 batched payment-total query for the whole page, got %d", f.paymentRepository.getTotalPaidByInvoiceIDsCallCount)
	}
}

// TestInvoiceService_List_EmptyResultSkipsCurrencyAndPaymentWork proves
// an empty page never bothers looking up settings or payment totals at
// all — there's nothing to resolve.
func TestInvoiceService_List_EmptyResultSkipsCurrencyAndPaymentWork(t *testing.T) {
	f := newTestFixture()

	f.settingsRepository.getByOrganisationIDCallCount = 0
	f.paymentRepository.getTotalPaidByInvoiceIDsCallCount = 0

	items, total, err := f.service.List(context.Background(), f.organisationID, InvoiceListFilter{Limit: 50}, time.Now())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("expected an empty result, got total=%d items=%+v", total, items)
	}
	if f.settingsRepository.getByOrganisationIDCallCount != 0 {
		t.Errorf("expected no settings lookup for an empty page, got %d calls", f.settingsRepository.getByOrganisationIDCallCount)
	}
	if f.paymentRepository.getTotalPaidByInvoiceIDsCallCount != 0 {
		t.Errorf("expected no payment-total query for an empty page, got %d calls", f.paymentRepository.getTotalPaidByInvoiceIDsCallCount)
	}
}
