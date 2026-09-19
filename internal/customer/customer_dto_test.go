package customer

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestToCustomerResponse_NormalizesNonUTCTimestampsToUTC is the
// Milestone 8 Part 2 regression test for the systemic non-UTC-timestamp
// defect Part 1 found.
func TestToCustomerResponse_NormalizesNonUTCTimestampsToUTC(t *testing.T) {
	bst := time.FixedZone("BST", 3600)
	nonUTC := time.Date(2026, 6, 15, 14, 30, 0, 0, bst)

	c := &Customer{
		ID:             uuid.New(),
		OrganisationID: uuid.New(),
		Name:           "Acme Ltd",
		Status:         "active",
		CreatedAt:      nonUTC,
		UpdatedAt:      nonUTC,
	}

	response := toCustomerResponse(c)

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

// TestToAddressResponse_NormalizesNonUTCTimestampsToUTC mirrors the
// above for a customer's billing AddressResponse.
func TestToAddressResponse_NormalizesNonUTCTimestampsToUTC(t *testing.T) {
	bst := time.FixedZone("BST", 3600)
	nonUTC := time.Date(2026, 6, 15, 14, 30, 0, 0, bst)

	a := &Address{
		ID:         uuid.New(),
		CustomerID: uuid.New(),
		Type:       "billing",
		Street:     "1 Acme Way",
		City:       "London",
		PostalCode: "E1 6AN",
		Country:    "GB",
		CreatedAt:  nonUTC,
		UpdatedAt:  nonUTC,
	}

	response := toAddressResponse(a)

	wantUTC := nonUTC.UTC().Format(time.RFC3339)

	if response.CreatedAt != wantUTC {
		t.Errorf("expected CreatedAt %q, got %q", wantUTC, response.CreatedAt)
	}
	if response.UpdatedAt != wantUTC {
		t.Errorf("expected UpdatedAt %q, got %q", wantUTC, response.UpdatedAt)
	}
}
