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

// --- MarkSent ---

func TestInvoice_MarkSent_DraftSucceeds(t *testing.T) {
	inv := draftInvoice()
	sentAt := time.Date(2026, 1, 15, 12, 30, 0, 0, time.UTC)

	if err := inv.MarkSent(sentAt); err != nil {
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

func TestInvoice_MarkSent_SentRejects(t *testing.T) {
	inv := draftInvoice()
	inv.Status = InvoiceStatusSent

	err := inv.MarkSent(time.Now().UTC())
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

	err := inv.MarkSent(time.Now().UTC())
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	if inv.Status != InvoiceStatusPaid {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusPaid, inv.Status)
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
