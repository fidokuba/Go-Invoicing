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
