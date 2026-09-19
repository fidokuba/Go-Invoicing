package invoice

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func draftInvoice() *Invoice {
	return &Invoice{
		ID:             uuid.New(),
		OrganisationID: uuid.New(),
		CustomerID:     uuid.New(),
		Status:         InvoiceStatusDraft,
	}
}

// strPtr is a small test helper for building *string snapshot fields.
func strPtr(s string) *string { return &s }

// validSnapshot returns a fully-populated InvoicePartySnapshot — every
// optional field present, not just the three required ones — for tests
// that don't care about the specific values, only that Send/MarkSent
// succeeds and copies them correctly.
func validSnapshot() InvoicePartySnapshot {
	return InvoicePartySnapshot{
		SellerName:       "Acme Ltd",
		SellerEmail:      strPtr("seller@acme.test"),
		SellerPhone:      strPtr("+44 20 7946 0958"),
		SellerWebsite:    strPtr("https://acme.test"),
		SellerAddress:    strPtr("1 Acme Way"),
		SellerCity:       strPtr("London"),
		SellerState:      strPtr("Greater London"),
		SellerPostalCode: strPtr("E1 6AN"),
		SellerCountry:    strPtr("GB"),
		SellerTaxID:      strPtr("GB123456789"),

		CustomerName:        "Bob's Bakery",
		CustomerCompanyName: strPtr("Bob's Bakery Ltd"),
		CustomerEmail:       strPtr("bob@bakery.test"),
		CustomerPhone:       strPtr("+44 161 496 0000"),
		CustomerTaxID:       strPtr("GB987654321"),
		CustomerAddress:     strPtr("2 Bakery Street"),
		CustomerCity:        strPtr("Manchester"),
		CustomerState:       strPtr("Greater Manchester"),
		CustomerPostalCode:  strPtr("M1 1AE"),
		CustomerCountry:     strPtr("GB"),

		Currency: "GBP",
	}
}

// minimalValidSnapshot has only the three required fields set — every
// optional field absent — proving Send/MarkSent succeeds without them
// (Milestone 7 Part 1 never required a billing address, and an
// organisation/customer may simply have no email/phone/etc. populated).
func minimalValidSnapshot() InvoicePartySnapshot {
	return InvoicePartySnapshot{
		SellerName:   "Acme Ltd",
		CustomerName: "Bob's Bakery",
		Currency:     "GBP",
	}
}

// --- MarkSent ---

func TestInvoice_MarkSent_DraftSucceeds(t *testing.T) {
	inv := draftInvoice()
	sentAt := time.Date(2026, 1, 15, 12, 30, 0, 0, time.UTC)

	if err := inv.MarkSent(sentAt, validSnapshot()); err != nil {
		t.Fatalf("expected Draft -> Sent to succeed, got %v", err)
	}

	if inv.Status != InvoiceStatusSent {
		t.Errorf("expected status %q, got %q", InvoiceStatusSent, inv.Status)
	}

	if inv.SentAt == nil {
		t.Fatal("expected SentAt to be populated")
	}

	if !inv.SentAt.Equal(sentAt) {
		t.Errorf("expected SentAt %v, got %v", sentAt, *inv.SentAt)
	}
}

// TestInvoice_MarkSent_CopiesSnapshotCorrectly proves every snapshot
// field — seller, customer, and currency alike — lands on the exact
// matching Invoice field, not merely that Send "succeeds".
func TestInvoice_MarkSent_CopiesSnapshotCorrectly(t *testing.T) {
	inv := draftInvoice()
	snapshot := validSnapshot()

	if err := inv.MarkSent(time.Now().UTC(), snapshot); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	checks := []struct {
		name string
		got  *string
		want *string
	}{
		{"SellerName", inv.SellerName, &snapshot.SellerName},
		{"SellerEmail", inv.SellerEmail, snapshot.SellerEmail},
		{"SellerPhone", inv.SellerPhone, snapshot.SellerPhone},
		{"SellerWebsite", inv.SellerWebsite, snapshot.SellerWebsite},
		{"SellerAddress", inv.SellerAddress, snapshot.SellerAddress},
		{"SellerCity", inv.SellerCity, snapshot.SellerCity},
		{"SellerState", inv.SellerState, snapshot.SellerState},
		{"SellerPostalCode", inv.SellerPostalCode, snapshot.SellerPostalCode},
		{"SellerCountry", inv.SellerCountry, snapshot.SellerCountry},
		{"SellerTaxID", inv.SellerTaxID, snapshot.SellerTaxID},
		{"CustomerName", inv.CustomerName, &snapshot.CustomerName},
		{"CustomerCompanyName", inv.CustomerCompanyName, snapshot.CustomerCompanyName},
		{"CustomerEmail", inv.CustomerEmail, snapshot.CustomerEmail},
		{"CustomerPhone", inv.CustomerPhone, snapshot.CustomerPhone},
		{"CustomerTaxID", inv.CustomerTaxID, snapshot.CustomerTaxID},
		{"CustomerAddress", inv.CustomerAddress, snapshot.CustomerAddress},
		{"CustomerCity", inv.CustomerCity, snapshot.CustomerCity},
		{"CustomerState", inv.CustomerState, snapshot.CustomerState},
		{"CustomerPostalCode", inv.CustomerPostalCode, snapshot.CustomerPostalCode},
		{"CustomerCountry", inv.CustomerCountry, snapshot.CustomerCountry},
		{"Currency", inv.Currency, &snapshot.Currency},
	}

	for _, c := range checks {
		if c.got == nil || c.want == nil || *c.got != *c.want {
			t.Errorf("%s: expected %v, got %v", c.name, derefOrNil(c.want), derefOrNil(c.got))
		}
	}
}

func derefOrNil(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// TestInvoice_MarkSent_OptionalFieldsAbsentSucceeds proves Send doesn't
// require every optional seller/customer/address field to be present —
// only SellerName, CustomerName and Currency are required.
func TestInvoice_MarkSent_OptionalFieldsAbsentSucceeds(t *testing.T) {
	inv := draftInvoice()

	if err := inv.MarkSent(time.Now().UTC(), minimalValidSnapshot()); err != nil {
		t.Fatalf("expected Draft -> Sent with only required fields to succeed, got %v", err)
	}

	if inv.SellerEmail != nil || inv.CustomerAddress != nil || inv.SellerTaxID != nil {
		t.Error("expected every unspecified optional snapshot field to remain nil")
	}
}

func TestInvoice_MarkSent_MissingSellerNameRejected(t *testing.T) {
	inv := draftInvoice()
	snapshot := minimalValidSnapshot()
	snapshot.SellerName = ""

	err := inv.MarkSent(time.Now().UTC(), snapshot)
	if !errors.Is(err, ErrInvoiceSnapshotSellerNameRequired) {
		t.Fatalf("expected ErrInvoiceSnapshotSellerNameRequired, got %v", err)
	}

	if inv.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q after a rejected snapshot, got %q", InvoiceStatusDraft, inv.Status)
	}
}

func TestInvoice_MarkSent_MissingCustomerNameRejected(t *testing.T) {
	inv := draftInvoice()
	snapshot := minimalValidSnapshot()
	snapshot.CustomerName = ""

	err := inv.MarkSent(time.Now().UTC(), snapshot)
	if !errors.Is(err, ErrInvoiceSnapshotCustomerNameRequired) {
		t.Fatalf("expected ErrInvoiceSnapshotCustomerNameRequired, got %v", err)
	}
}

func TestInvoice_MarkSent_MissingCurrencyRejected(t *testing.T) {
	inv := draftInvoice()
	snapshot := minimalValidSnapshot()
	snapshot.Currency = ""

	err := inv.MarkSent(time.Now().UTC(), snapshot)
	if !errors.Is(err, ErrInvoiceSnapshotCurrencyRequired) {
		t.Fatalf("expected ErrInvoiceSnapshotCurrencyRequired, got %v", err)
	}
}

func TestInvoice_MarkSent_InvalidCurrencyRejected(t *testing.T) {
	inv := draftInvoice()
	snapshot := minimalValidSnapshot()
	snapshot.Currency = "gbp" // lowercase — not the normalized shape

	err := inv.MarkSent(time.Now().UTC(), snapshot)
	if !errors.Is(err, ErrInvoiceSnapshotCurrencyInvalid) {
		t.Fatalf("expected ErrInvoiceSnapshotCurrencyInvalid, got %v", err)
	}
}

func TestInvoice_MarkSent_SentRejects(t *testing.T) {
	inv := draftInvoice()
	inv.Status = InvoiceStatusSent

	err := inv.MarkSent(time.Now().UTC(), validSnapshot())
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	if inv.SentAt != nil {
		t.Error("expected SentAt to remain nil after a rejected transition")
	}
}

func TestInvoice_MarkSent_PaidRejects(t *testing.T) {
	inv := draftInvoice()
	inv.Status = InvoiceStatusPaid

	err := inv.MarkSent(time.Now().UTC(), validSnapshot())
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	if inv.Status != InvoiceStatusPaid {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusPaid, inv.Status)
	}
}

// TestInvoice_MarkSent_NonDraftCannotRecaptureSnapshot proves the guard
// order: a non-Draft invoice is rejected before the snapshot is ever
// examined, so an already-Sent invoice's existing snapshot fields are
// left completely untouched by a repeat MarkSent call — even one given
// deliberately different snapshot data.
func TestInvoice_MarkSent_NonDraftCannotRecaptureSnapshot(t *testing.T) {
	inv := draftInvoice()
	firstSnapshot := validSnapshot()
	firstSentAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := inv.MarkSent(firstSentAt, firstSnapshot); err != nil {
		t.Fatalf("first mark sent: %v", err)
	}

	differentSnapshot := validSnapshot()
	differentSnapshot.SellerName = "A Totally Different Seller"
	differentSnapshot.CustomerName = "A Totally Different Customer"
	differentSnapshot.Currency = "USD"

	err := inv.MarkSent(time.Now().UTC(), differentSnapshot)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	if *inv.SellerName != firstSnapshot.SellerName {
		t.Errorf("expected SellerName to remain %q, got %q", firstSnapshot.SellerName, *inv.SellerName)
	}

	if *inv.CustomerName != firstSnapshot.CustomerName {
		t.Errorf("expected CustomerName to remain %q, got %q", firstSnapshot.CustomerName, *inv.CustomerName)
	}

	if *inv.Currency != firstSnapshot.Currency {
		t.Errorf("expected Currency to remain %q, got %q", firstSnapshot.Currency, *inv.Currency)
	}

	if !inv.SentAt.Equal(firstSentAt) {
		t.Errorf("expected SentAt to remain %v, got %v", firstSentAt, *inv.SentAt)
	}
}

// --- CanAcceptPayment ---

func TestInvoice_CanAcceptPayment(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{InvoiceStatusDraft, false},
		{InvoiceStatusSent, true},
		{InvoiceStatusPaid, false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			inv := draftInvoice()
			inv.Status = tt.status

			if got := inv.CanAcceptPayment(); got != tt.want {
				t.Errorf("status %q: expected CanAcceptPayment() = %v, got %v", tt.status, tt.want, got)
			}
		})
	}
}

// --- EffectiveStatus ---

func TestInvoice_EffectiveStatus(t *testing.T) {
	dueDate := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	dayBefore := dueDate.AddDate(0, 0, -1)
	onDueDate := dueDate
	dayAfter := dueDate.AddDate(0, 0, 1)

	tests := []struct {
		name    string
		status  string
		dueDate time.Time
		now     time.Time
		want    string
	}{
		{"Draft with future due date", InvoiceStatusDraft, dueDate, dayBefore, InvoiceStatusDraft},
		{"Draft with past due date", InvoiceStatusDraft, dueDate, dayAfter, InvoiceStatusDraft},
		{"Sent before due date", InvoiceStatusSent, dueDate, dayBefore, InvoiceStatusSent},
		{"Sent on due date", InvoiceStatusSent, dueDate, onDueDate, InvoiceStatusSent},
		{"Sent after due date", InvoiceStatusSent, dueDate, dayAfter, InvoiceStatusOverdue},
		{"Paid before due date", InvoiceStatusPaid, dueDate, dayBefore, InvoiceStatusPaid},
		{"Paid after due date", InvoiceStatusPaid, dueDate, dayAfter, InvoiceStatusPaid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := draftInvoice()
			inv.Status = tt.status
			inv.DueDate = tt.dueDate

			if got := inv.EffectiveStatus(tt.now); got != tt.want {
				t.Errorf("expected EffectiveStatus %q, got %q", tt.want, got)
			}
		})
	}
}

// TestInvoice_EffectiveStatus_BoundaryAroundDueDate is the explicit
// day-before/on/day-after boundary sweep Milestone 5 calls for, using
// times of day deliberately far from midnight to prove the comparison is
// calendar-date-based, not a naive full-timestamp comparison — an
// invoice must not appear overdue merely because "now" is later in the
// clock day than DueDate's stored midnight.
func TestInvoice_EffectiveStatus_BoundaryAroundDueDate(t *testing.T) {
	dueDate := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"day before due date, evening", time.Date(2026, 3, 9, 23, 59, 59, 0, time.UTC), InvoiceStatusSent},
		{"on due date, midnight", time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), InvoiceStatusSent},
		{"on due date, end of day", time.Date(2026, 3, 10, 23, 59, 59, 0, time.UTC), InvoiceStatusSent},
		{"day after due date, just past midnight", time.Date(2026, 3, 11, 0, 0, 1, 0, time.UTC), InvoiceStatusOverdue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := draftInvoice()
			inv.Status = InvoiceStatusSent
			inv.DueDate = dueDate

			if got := inv.EffectiveStatus(tt.now); got != tt.want {
				t.Errorf("now=%v: expected EffectiveStatus %q, got %q", tt.now, tt.want, got)
			}
		})
	}
}

// TestInvoice_EffectiveStatus_PersistedOverduePassesThrough documents the
// defensive handling for a persisted "overdue" value: Milestone 5 never
// writes one, but if it exists (e.g. a manually inserted row, or a
// future migration), EffectiveStatus reports it as-is rather than
// re-deriving it from DueDate — it is not re-evaluated because
// EffectiveStatus's due-date branch only ever runs for a persisted Sent
// invoice.
func TestInvoice_EffectiveStatus_PersistedOverduePassesThrough(t *testing.T) {
	inv := draftInvoice()
	inv.Status = InvoiceStatusOverdue
	inv.DueDate = time.Now().UTC().AddDate(1, 0, 0) // a whole year in the future

	if got := inv.EffectiveStatus(time.Now().UTC()); got != InvoiceStatusOverdue {
		t.Errorf("expected a persisted Overdue status to pass through unchanged, got %q", got)
	}
}
