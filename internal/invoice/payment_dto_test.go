package invoice

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestToPaymentResponse_NormalizesNonUTCTimestampsToUTC is the
// Milestone 8 Part 2 regression test for the systemic non-UTC-timestamp
// defect Part 1 found.
func TestToPaymentResponse_NormalizesNonUTCTimestampsToUTC(t *testing.T) {
	bst := time.FixedZone("BST", 3600)
	nonUTC := time.Date(2026, 6, 15, 14, 30, 0, 0, bst)

	p := &Payment{
		ID:            uuid.New(),
		InvoiceID:     uuid.New(),
		Amount:        1000,
		PaymentMethod: "cash",
		PaymentDate:   time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		CreatedAt:     nonUTC,
		UpdatedAt:     nonUTC,
	}

	response := toPaymentResponse(p)

	wantUTC := nonUTC.UTC().Format(time.RFC3339)

	if response.CreatedAt != wantUTC {
		t.Errorf("expected CreatedAt %q, got %q", wantUTC, response.CreatedAt)
	}
	if response.UpdatedAt != wantUTC {
		t.Errorf("expected UpdatedAt %q, got %q", wantUTC, response.UpdatedAt)
	}
	if !strings.HasSuffix(response.CreatedAt, "Z") || !strings.HasSuffix(response.UpdatedAt, "Z") {
		t.Errorf("expected both timestamps to end in Z (UTC), got CreatedAt=%q UpdatedAt=%q", response.CreatedAt, response.UpdatedAt)
	}
}
