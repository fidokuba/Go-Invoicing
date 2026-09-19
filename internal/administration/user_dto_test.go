package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestToUserResponse_NormalizesNonUTCTimestampsToUTC is the Milestone 8
// Part 2 regression test for the systemic non-UTC-timestamp defect Part 1
// found: every timestamp field must be normalized to UTC (and so end in
// "Z") before RFC3339 formatting, regardless of what zone the underlying
// time.Time carries — which, for a value read back from pgx, depends on
// the server process's local zone unless this normalization happens.
func TestToUserResponse_NormalizesNonUTCTimestampsToUTC(t *testing.T) {
	bst := time.FixedZone("BST", 3600) // UTC+1, e.g. British Summer Time
	nonUTC := time.Date(2026, 6, 15, 14, 30, 0, 0, bst)

	u := &User{
		ID:             uuid.New(),
		OrganisationID: uuid.New(),
		Name:           "Alice",
		Email:          "alice@example.com",
		Role:           UserRoleUser,
		IsActive:       true,
		LastLogin:      &nonUTC,
		CreatedAt:      nonUTC,
		UpdatedAt:      nonUTC,
	}

	response := toUserResponse(u)

	wantUTC := nonUTC.UTC().Format(time.RFC3339)

	if response.CreatedAt != wantUTC {
		t.Errorf("expected CreatedAt %q, got %q", wantUTC, response.CreatedAt)
	}
	if response.UpdatedAt != wantUTC {
		t.Errorf("expected UpdatedAt %q, got %q", wantUTC, response.UpdatedAt)
	}
	if response.LastLogin == nil || *response.LastLogin != wantUTC {
		t.Errorf("expected LastLogin %q, got %v", wantUTC, response.LastLogin)
	}

	for name, value := range map[string]string{
		"CreatedAt": response.CreatedAt,
		"UpdatedAt": response.UpdatedAt,
		"LastLogin": *response.LastLogin,
	} {
		if !strings.HasSuffix(value, "Z") {
			t.Errorf("expected %s to end in Z (UTC), got %q", name, value)
		}
	}
}
