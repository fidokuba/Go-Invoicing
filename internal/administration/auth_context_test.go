package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestAuthenticatedUserFromContext_RoundTrip(t *testing.T) {
	identity := AuthenticatedUser{
		UserID:         uuid.New(),
		OrganisationID: uuid.New(),
		Role:           UserRoleAdmin,
	}

	ctx := WithAuthenticatedUser(context.Background(), identity)

	got, ok := AuthenticatedUserFromContext(ctx)
	if !ok {
		t.Fatal("expected ok to be true after WithAuthenticatedUser")
	}

	if got != identity {
		t.Errorf("expected %+v, got %+v", identity, got)
	}
}

func TestAuthenticatedUserFromContext_MissingIdentityReturnsFalse(t *testing.T) {
	got, ok := AuthenticatedUserFromContext(context.Background())
	if ok {
		t.Fatalf("expected ok to be false for a context with no identity attached, got %+v", got)
	}

	if got != (AuthenticatedUser{}) {
		t.Errorf("expected a zero-value AuthenticatedUser, got %+v", got)
	}
}
