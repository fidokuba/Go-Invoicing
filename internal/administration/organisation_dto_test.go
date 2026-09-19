package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestToOrganisationResponse_NormalizesNonUTCTimestampsToUTC mirrors
// TestToUserResponse_NormalizesNonUTCTimestampsToUTC for
// OrganisationResponse (Milestone 8 Part 2).
func TestToOrganisationResponse_NormalizesNonUTCTimestampsToUTC(t *testing.T) {
	bst := time.FixedZone("BST", 3600)
	nonUTC := time.Date(2026, 6, 15, 14, 30, 0, 0, bst)

	organisation := &Organisation{
		ID:        uuid.New(),
		Name:      "Acme Ltd",
		CreatedAt: nonUTC,
		UpdatedAt: nonUTC,
	}

	response := toOrganisationResponse(organisation)

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
