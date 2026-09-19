package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedUser(t *testing.T, db *pgxpool.Pool, repository *PostgresUserRepository, organisationID uuid.UUID, email string, mutate func(*User)) *User {
	t.Helper()

	u := newTestUser(organisationID, email)
	if mutate != nil {
		mutate(u)
	}

	if err := repository.Create(context.Background(), u); err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", u.ID)
	})

	return u
}

func TestPostgresUserRepository_List_TenantIsolation(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresUserRepository(db)
	orgA := createTestOrganisation(t, db)
	orgB := createTestOrganisation(t, db)

	seedUser(t, db, repository, orgA, "orga-user@example.com", nil)
	seedUser(t, db, repository, orgB, "orgb-user@example.com", nil)

	items, total, err := repository.List(ctx, orgA, UserListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Email != "orga-user@example.com" {
		t.Fatalf("expected only Org A's user, got total=%d items=%+v", total, items)
	}
}

func TestPostgresUserRepository_List_RoleAndActiveFilters(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresUserRepository(db)
	org := createTestOrganisation(t, db)

	seedUser(t, db, repository, org, "admin1@example.com", func(u *User) { u.Role = UserRoleAdmin })
	seedUser(t, db, repository, org, "manager1@example.com", func(u *User) { u.Role = UserRoleManager })
	seedUser(t, db, repository, org, "user1@example.com", func(u *User) { u.Role = UserRoleUser })
	seedUser(t, db, repository, org, "inactive1@example.com", func(u *User) {
		u.Role = UserRoleUser
		u.IsActive = false
	})

	t.Run("role filter", func(t *testing.T) {
		items, total, err := repository.List(ctx, org, UserListFilter{Role: UserRoleAdmin, Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 || items[0].Email != "admin1@example.com" {
			t.Fatalf("expected only the admin, got total=%d items=%+v", total, items)
		}
	})

	t.Run("active filter", func(t *testing.T) {
		active := false
		items, total, err := repository.List(ctx, org, UserListFilter{Active: &active, Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 || items[0].Email != "inactive1@example.com" {
			t.Fatalf("expected only the inactive user, got total=%d items=%+v", total, items)
		}
	})

	t.Run("role and active combined", func(t *testing.T) {
		active := true
		_, total, err := repository.List(ctx, org, UserListFilter{Role: UserRoleUser, Active: &active, Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 {
			t.Fatalf("expected exactly the one active plain user, got %d", total)
		}
	})
}

// TestPostgresUserRepository_List_SortByEmailWithTieBreak proves sort
// plus deterministic id-ascending tie-breaking against real Postgres,
// using createdAt (non-unique) as the tied field.
func TestPostgresUserRepository_List_SortByEmailWithTieBreak(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresUserRepository(db)
	org := createTestOrganisation(t, db)

	seedUser(t, db, repository, org, "aaa@example.com", nil)
	seedUser(t, db, repository, org, "zzz@example.com", nil)
	seedUser(t, db, repository, org, "mmm@example.com", nil)

	items, _, err := repository.List(ctx, org, UserListFilter{Sort: "email", Order: "asc", Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 users, got %d", len(items))
	}
	wantOrder := []string{"aaa@example.com", "mmm@example.com", "zzz@example.com"}
	for i, want := range wantOrder {
		if items[i].Email != want {
			t.Errorf("position %d: expected %q, got %q", i, want, items[i].Email)
		}
	}
}
