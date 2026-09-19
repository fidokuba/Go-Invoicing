package product

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedProduct(t *testing.T, db *pgxpool.Pool, repository *PostgresProductRepository, organisationID uuid.UUID, name, sku string, mutate func(*Product)) *Product {
	t.Helper()

	p := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           name,
		SKU:            sku,
		Price:          1000,
		IsActive:       true,
	}
	if mutate != nil {
		mutate(p)
	}

	if err := repository.Create(context.Background(), p); err != nil {
		t.Fatalf("create product %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM products WHERE id = $1", p.ID)
	})

	return p
}

func TestPostgresProductRepository_List_TenantIsolation(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresProductRepository(db)
	orgA := createTestOrganisation(t, db)
	orgB := createTestOrganisation(t, db)

	seedProduct(t, db, repository, orgA, "Org A Widget", "SKU-A-1", nil)
	seedProduct(t, db, repository, orgB, "Org B Widget", "SKU-B-1", nil)

	items, total, err := repository.List(ctx, orgA, ListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Org A Widget" {
		t.Fatalf("expected only Org A's product, got total=%d items=%+v", total, items)
	}
}

func TestPostgresProductRepository_List_SearchNameAndSKU(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresProductRepository(db)
	org := createTestOrganisation(t, db)

	seedProduct(t, db, repository, org, "Blue Widget", "SKU-BLU-1", nil)
	seedProduct(t, db, repository, org, "Red Gadget", "SKU-RED-WIDGET", nil)
	seedProduct(t, db, repository, org, "Green Gizmo", "SKU-GRN-1", nil)

	t.Run("matches name case-insensitively", func(t *testing.T) {
		_, total, err := repository.List(ctx, org, ListFilter{Search: "widget", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 2 {
			t.Fatalf("expected 2 matches (name 'Blue Widget' + sku 'SKU-RED-WIDGET'), got %d", total)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		_, total, err := repository.List(ctx, org, ListFilter{Search: "nonexistent", Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 0 {
			t.Fatalf("expected no matches, got %d", total)
		}
	})
}

func TestPostgresProductRepository_List_IsActiveFilter(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresProductRepository(db)
	org := createTestOrganisation(t, db)

	seedProduct(t, db, repository, org, "Active Product", "SKU-ACT-1", func(p *Product) { p.IsActive = true })
	seedProduct(t, db, repository, org, "Inactive Product", "SKU-INA-1", func(p *Product) { p.IsActive = false })

	active := true
	items, total, err := repository.List(ctx, org, ListFilter{IsActive: &active, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || items[0].Name != "Active Product" {
		t.Fatalf("expected only the active product, got total=%d items=%+v", total, items)
	}

	inactive := false
	items, total, err = repository.List(ctx, org, ListFilter{IsActive: &inactive, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || items[0].Name != "Inactive Product" {
		t.Fatalf("expected only the inactive product, got total=%d items=%+v", total, items)
	}
}

// TestPostgresProductRepository_List_SortByPriceWithTieBreak proves
// numeric sort (price) plus deterministic id-ascending tie-breaking
// against real Postgres.
func TestPostgresProductRepository_List_SortByPriceWithTieBreak(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresProductRepository(db)
	org := createTestOrganisation(t, db)

	first := seedProduct(t, db, repository, org, "Tied A", "SKU-TIE-1", func(p *Product) { p.Price = 500 })
	second := seedProduct(t, db, repository, org, "Tied B", "SKU-TIE-2", func(p *Product) { p.Price = 500 })
	seedProduct(t, db, repository, org, "Expensive", "SKU-EXP-1", func(p *Product) { p.Price = 9999 })

	wantFirst, wantSecond := first.ID, second.ID
	if wantSecond.String() < wantFirst.String() {
		wantFirst, wantSecond = wantSecond, wantFirst
	}

	items, _, err := repository.List(ctx, org, ListFilter{Sort: "price", Order: "asc", Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].ID != wantFirst || items[1].ID != wantSecond {
		t.Errorf("expected tied-price rows in id-ascending order, got %s then %s", items[0].ID, items[1].ID)
	}
	if items[2].Name != "Expensive" {
		t.Errorf("expected 'Expensive' last ascending by price, got %+v", items[2])
	}
}

func TestPostgresProductRepository_List_EmptyAndOffsetBeyondTotal(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresProductRepository(db)
	org := createTestOrganisation(t, db)

	seedProduct(t, db, repository, org, "Only Product", "SKU-ONLY-1", nil)

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
}
