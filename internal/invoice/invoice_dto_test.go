package invoice

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestToInvoiceResponse_NormalizesNonUTCTimestampsToUTC is the
// Milestone 8 Part 2 regression test for the systemic non-UTC-timestamp
// defect Part 1 found: CreatedAt, UpdatedAt and SentAt must all be
// normalized to UTC (ending in "Z") regardless of the source time.Time's
// original zone.
func TestToInvoiceResponse_NormalizesNonUTCTimestampsToUTC(t *testing.T) {
	bst := time.FixedZone("BST", 3600)
	nonUTC := time.Date(2026, 6, 15, 14, 30, 0, 0, bst)

	inv := &Invoice{
		ID:             uuid.New(),
		OrganisationID: uuid.New(),
		CustomerID:     uuid.New(),
		InvoiceNumber:  "INV-1",
		IssueDate:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DueDate:        time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Status:         InvoiceStatusSent,
		SentAt:         &nonUTC,
		CreatedAt:      nonUTC,
		UpdatedAt:      nonUTC,
	}

	response := toInvoiceResponse(inv, nil, 0, "GBP", time.Now().UTC())

	wantUTC := nonUTC.UTC().Format(time.RFC3339)

	if response.CreatedAt != wantUTC {
		t.Errorf("expected CreatedAt %q, got %q", wantUTC, response.CreatedAt)
	}
	if response.UpdatedAt != wantUTC {
		t.Errorf("expected UpdatedAt %q, got %q", wantUTC, response.UpdatedAt)
	}
	if response.SentAt == nil || *response.SentAt != wantUTC {
		t.Errorf("expected SentAt %q, got %v", wantUTC, response.SentAt)
	}

	for name, value := range map[string]string{
		"CreatedAt": response.CreatedAt,
		"UpdatedAt": response.UpdatedAt,
		"SentAt":    *response.SentAt,
	} {
		if !strings.HasSuffix(value, "Z") {
			t.Errorf("expected %s to end in Z (UTC), got %q", name, value)
		}
	}
}

// TestToInvoiceListItemResponse_FieldMappingAndUTCNormalization is the
// Milestone 8 Part 3 amendment's DTO-level test: every field
// InvoiceListItemResponse documents must be populated correctly from an
// InvoiceListItem, timestamps normalized to UTC exactly like
// toInvoiceResponse, and — the whole point of this amendment — the
// struct has no way to express a "lines" value at all.
func TestToInvoiceListItemResponse_FieldMappingAndUTCNormalization(t *testing.T) {
	bst := time.FixedZone("BST", 3600)
	nonUTC := time.Date(2026, 6, 15, 14, 30, 0, 0, bst)
	customerID := uuid.New()

	// A due date a full year past "now" so this test never becomes flaky
	// as real time passes — the invoice must read as "sent", not
	// "overdue".
	issueDate := time.Now().UTC().Truncate(24 * time.Hour)
	dueDate := issueDate.AddDate(1, 0, 0)

	inv := &Invoice{
		ID:             uuid.New(),
		OrganisationID: uuid.New(),
		CustomerID:     customerID,
		InvoiceNumber:  "INV-42",
		IssueDate:      issueDate,
		DueDate:        dueDate,
		Subtotal:       10000,
		VATTotal:       2000,
		Total:          12000,
		Status:         InvoiceStatusSent,
		SentAt:         &nonUTC,
		CreatedAt:      nonUTC,
		UpdatedAt:      nonUTC,
	}

	item := InvoiceListItem{Invoice: inv, AmountPaid: 4000, Currency: "GBP"}

	response := toInvoiceListItemResponse(item, time.Now().UTC())

	if response.ID != inv.ID.String() {
		t.Errorf("expected ID %q, got %q", inv.ID.String(), response.ID)
	}
	if response.InvoiceNumber != "INV-42" {
		t.Errorf("expected InvoiceNumber %q, got %q", "INV-42", response.InvoiceNumber)
	}
	if response.CustomerID != customerID.String() {
		t.Errorf("expected CustomerID %q, got %q", customerID.String(), response.CustomerID)
	}
	if response.IssueDate != issueDate.Format(dateLayout) || response.DueDate != dueDate.Format(dateLayout) {
		t.Errorf("expected dates %s/%s, got %s/%s", issueDate.Format(dateLayout), dueDate.Format(dateLayout), response.IssueDate, response.DueDate)
	}
	if response.Status != "sent" {
		t.Errorf("expected status %q, got %q", "sent", response.Status)
	}
	if response.Currency != "GBP" {
		t.Errorf("expected currency %q, got %q", "GBP", response.Currency)
	}
	if response.Subtotal != 10000 || response.VATTotal != 2000 || response.Total != 12000 {
		t.Errorf("expected subtotal/vatTotal/total 10000/2000/12000, got %d/%d/%d", response.Subtotal, response.VATTotal, response.Total)
	}
	if response.AmountPaid != 4000 {
		t.Errorf("expected amountPaid 4000, got %d", response.AmountPaid)
	}
	if response.AmountOutstanding != 8000 {
		t.Errorf("expected amountOutstanding 8000, got %d", response.AmountOutstanding)
	}

	wantUTC := nonUTC.UTC().Format(time.RFC3339)
	if response.SentAt == nil || *response.SentAt != wantUTC {
		t.Errorf("expected SentAt %q, got %v", wantUTC, response.SentAt)
	}
	if response.CreatedAt != wantUTC {
		t.Errorf("expected CreatedAt %q, got %q", wantUTC, response.CreatedAt)
	}
	if response.UpdatedAt != wantUTC {
		t.Errorf("expected UpdatedAt %q, got %q", wantUTC, response.UpdatedAt)
	}
}
