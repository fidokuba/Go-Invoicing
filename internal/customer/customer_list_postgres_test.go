package customer

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedCustomer creates a customer directly via the repository under test
// and registers cleanup for it.
func seedCustomer(t *testing.T, db *pgxpool.Pool, repository *PostgresCustomerRepository, organisationID uuid.UUID, name string, mutate func(*Customer)) *Customer {
	t.Helper()

	c := &Customer{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           name,
		Status:         CustomerStatusActive,
	}
	if mutate != nil {
		mutate(c)
	}

	if err := repository.Create(context.Background(), c); err != nil {
		t.Fatalf("create customer %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM customers WHERE id = $1", c.ID)
	})

	return c
}

func strPtr(s string) *string { return &s }

// TestPostgresCustomerRepository_List_TenantIsolation proves a
// customer belonging to a different organisation never appears in
// another organisation's list, nor counts toward its total.
func TestPostgresCustomerRepository_List_TenantIsolation(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repository := NewPostgresCustomerRepository(db)
	orgA := createTestOrganisation(t, db)
	orgB := createTestOrganisation(t, db)

	seedCustomer(t, db, repository, orgA, "Org A Customer", nil)
	seedCustomer(t, db, repository, orgB, "Org B Customer", nil)

	items, total, err := repository.List(ctx, orgA, ListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(items) != 1 || items[0].Name != "Org A Customer" {
		t.Fatalf("expected only Org A's customer, got %+v", items)
	}
}

// TestPostgresCustomerRepository_List_Search proves case-insensitive
// substring matching across name, company_name and email, tenant
// isolation combined with search, and the no-matches case.
func TestPostgresCustomerRepository_List_Search(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repository := NewPostgresCustomerRepository(db)
	orgA := createTestOrganisation(t, db)
	orgB := createTestOrganisation(t, db)

	seedCustomer(t, db, repository, orgA, "Acme Widgets", func(c *Customer) {
		c.Email = strPtr("hello@acme.test")
	})
	seedCustomer(t, db, repository, orgA, "Beta Corp", func(c *Customer) {
		c.CompanyName = strPtr("Acme Holdings")
	})
	seedCustomer(t, db, repository, orgA, "Gamma Inc", nil)
	// Same search term, different organisation — must never appear.
	seedCustomer(t, db, repository, orgB, "Acme Impostor", nil)

	t.Run("matches name case-insensitively", func(t *testing.T) {
		items, total, err := repository.List(ctx, orgA, ListFilter{Search: "acme", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 2 {
			t.Fatalf("expected 2 matches (name + companyName), got %d: %+v", total, items)
		}
	})

	t.Run("matches company name substring", func(t *testing.T) {
		items, total, err := repository.List(ctx, orgA, ListFilter{Search: "Holdings", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 || items[0].Name != "Beta Corp" {
			t.Fatalf("expected Beta Corp via company name match, got %+v (total %d)", items, total)
		}
	})

	t.Run("matches email substring", func(t *testing.T) {
		_, total, err := repository.List(ctx, orgA, ListFilter{Search: "hello@acme", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 {
			t.Fatalf("expected 1 match via email, got %d", total)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		items, total, err := repository.List(ctx, orgA, ListFilter{Search: "nonexistent-xyz", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 0 || len(items) != 0 {
			t.Fatalf("expected no matches, got total=%d items=%+v", total, items)
		}
	})

	t.Run("search combined with status filter", func(t *testing.T) {
		_, total, err := repository.List(ctx, orgA, ListFilter{Search: "acme", Status: CustomerStatusActive, Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 2 {
			t.Fatalf("expected 2 active matches for 'acme', got %d", total)
		}
	})
}

// TestPostgresCustomerRepository_List_StatusFilter proves ?status=
// actually narrows results, and that pagination.total respects it.
func TestPostgresCustomerRepository_List_StatusFilter(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repository := NewPostgresCustomerRepository(db)
	org := createTestOrganisation(t, db)

	seedCustomer(t, db, repository, org, "Active One", func(c *Customer) { c.Status = CustomerStatusActive })
	seedCustomer(t, db, repository, org, "Active Two", func(c *Customer) { c.Status = CustomerStatusActive })
	seedCustomer(t, db, repository, org, "Inactive One", func(c *Customer) { c.Status = CustomerStatusInactive })

	items, total, err := repository.List(ctx, org, ListFilter{Status: CustomerStatusInactive, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Inactive One" {
		t.Fatalf("expected exactly the one inactive customer, got total=%d items=%+v", total, items)
	}
}

// TestPostgresCustomerRepository_List_SortAscDescAndTieBreak proves
// ascending/descending sort and, critically, deterministic tie-breaking
// (id ascending) when two rows share the same primary sort value —
// against real Postgres, since ordering guarantees are a genuine SQL
// concern a fake can't meaningfully prove.
func TestPostgresCustomerRepository_List_SortAscDescAndTieBreak(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repository := NewPostgresCustomerRepository(db)
	org := createTestOrganisation(t, db)

	// Two customers with the identical name (tie on the primary sort
	// field) so id ascending must be what breaks the tie.
	first := seedCustomer(t, db, repository, org, "Same Name", nil)
	second := seedCustomer(t, db, repository, org, "Same Name", nil)
	seedCustomer(t, db, repository, org, "Zzz Last", nil)

	wantFirstID, wantSecondID := first.ID, second.ID
	if wantSecondID.String() < wantFirstID.String() {
		wantFirstID, wantSecondID = wantSecondID, wantFirstID
	}

	t.Run("ascending with tie-break", func(t *testing.T) {
		items, _, err := repository.List(ctx, org, ListFilter{Sort: "name", Order: "asc", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		if items[0].ID != wantFirstID || items[1].ID != wantSecondID {
			t.Errorf("expected tied rows in id-ascending order (%s, %s), got (%s, %s)",
				wantFirstID, wantSecondID, items[0].ID, items[1].ID)
		}
		if items[2].Name != "Zzz Last" {
			t.Errorf("expected 'Zzz Last' sorted after 'Same Name' ascending, got %+v", items[2])
		}
	})

	t.Run("descending", func(t *testing.T) {
		items, _, err := repository.List(ctx, org, ListFilter{Sort: "name", Order: "desc", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if items[0].Name != "Zzz Last" {
			t.Errorf("expected 'Zzz Last' first descending, got %+v", items[0])
		}
	})
}

// TestPostgresCustomerRepository_List_EmptyAndOffsetBeyondTotal proves
// an empty match set and an offset past the end of the result set both
// return 200-shaped, empty-but-valid results (never an error) with the
// correct, unchanged total.
func TestPostgresCustomerRepository_List_EmptyAndOffsetBeyondTotal(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repository := NewPostgresCustomerRepository(db)
	org := createTestOrganisation(t, db)

	seedCustomer(t, db, repository, org, "Only Customer", nil)

	t.Run("no customers at all for a fresh organisation", func(t *testing.T) {
		emptyOrg := createTestOrganisation(t, db)
		items, total, err := repository.List(ctx, emptyOrg, ListFilter{Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 0 || len(items) != 0 {
			t.Fatalf("expected empty result, got total=%d items=%+v", total, items)
		}
	})

	t.Run("offset beyond total returns empty items but unchanged total", func(t *testing.T) {
		items, total, err := repository.List(ctx, org, ListFilter{Limit: 50, Offset: 1000})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 {
			t.Fatalf("expected total to remain 1, got %d", total)
		}
		if len(items) != 0 {
			t.Fatalf("expected 0 items at an out-of-range offset, got %d", len(items))
		}
	})
}
