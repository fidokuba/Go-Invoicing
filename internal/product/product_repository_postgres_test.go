package product

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createTestOrganisation inserts a minimal organisation row directly (not
// via the administration package, to avoid a cross-package test
// dependency) and registers cleanup for it.
func createTestOrganisation(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()

	organisationID := uuid.New()

	_, err := db.Exec(
		context.Background(),
		"INSERT INTO organisations (id, name) VALUES ($1, $2)",
		organisationID,
		"Test Organisation",
	)
	if err != nil {
		t.Fatalf("create test organisation: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM organisations WHERE id = $1",
			organisationID,
		)
	})

	return organisationID
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	// t.Cleanup (not defer) so this runs after row-delete cleanups
	// registered later: Cleanup callbacks fire last-added-first, all
	// after the test function's own defers have already run.
	t.Cleanup(func() { db.Close() })

	return db
}

func TestPostgresProductRepository_CreateAndGetByID(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresProductRepository(db)

	description := "A very useful widget"
	category := "Hardware"
	p := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Test Product",
		Description:    &description,
		SKU:            "SKU-TEST-1",
		Price:          1999,
		Category:       &category,
		IsActive:       true,
	}

	if err := repository.Create(ctx, p); err != nil {
		t.Fatalf("create product: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM products WHERE id = $1", p.ID)
	})

	created, err := repository.GetByID(ctx, organisationID, p.ID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}

	if created.ID != p.ID {
		t.Errorf("expected ID %v, got %v", p.ID, created.ID)
	}

	if created.Name != p.Name {
		t.Errorf("expected name %q, got %q", p.Name, created.Name)
	}

	if created.SKU != p.SKU {
		t.Errorf("expected SKU %q, got %q", p.SKU, created.SKU)
	}

	if created.Price != p.Price {
		t.Errorf("expected price %d, got %d", p.Price, created.Price)
	}

	if created.Description == nil || *created.Description != description {
		t.Errorf("expected description %q, got %v", description, created.Description)
	}

	if created.Category == nil || *created.Category != category {
		t.Errorf("expected category %q, got %v", category, created.Category)
	}

	if !created.IsActive {
		t.Error("expected product to be active")
	}
}

func TestPostgresProductRepository_GetByID_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresProductRepository(db)

	p, err := repository.GetByID(ctx, organisationID, uuid.New())
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}

	if p != nil {
		t.Errorf("expected nil product, got %+v", p)
	}
}

// TestPostgresProductRepository_GetByID_OrganisationScoping proves that a
// product belonging to one organisation cannot be retrieved through a
// different organisation's ID.
func TestPostgresProductRepository_GetByID_OrganisationScoping(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	repository := NewPostgresProductRepository(db)

	p := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationA,
		Name:           "Test Product",
		SKU:            "SKU-SCOPE-1",
		Price:          1999,
		IsActive:       true,
	}

	if err := repository.Create(ctx, p); err != nil {
		t.Fatalf("create product: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM products WHERE id = $1", p.ID)
	})

	if _, err := repository.GetByID(ctx, organisationA, p.ID); err != nil {
		t.Fatalf("get product via owning organisation: %v", err)
	}

	result, err := repository.GetByID(ctx, organisationB, p.ID)
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound when scoped to the wrong organisation, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil product, got %+v", result)
	}
}

// TestPostgresProductRepository_Create_SKUUniquePerOrganisation proves the
// products_organisation_sku_unique constraint: the same SKU is rejected
// (as ErrProductSKUAlreadyExists, not a raw PostgreSQL error) within one
// organisation, but is fine reused in a different organisation.
func TestPostgresProductRepository_Create_SKUUniquePerOrganisation(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	repository := NewPostgresProductRepository(db)

	first := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationA,
		Name:           "First Product",
		SKU:            "SKU-DUP-1",
		Price:          1000,
		IsActive:       true,
	}

	if err := repository.Create(ctx, first); err != nil {
		t.Fatalf("create first product: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM products WHERE id = $1", first.ID)
	})

	// Same SKU, same organisation: must fail on the unique constraint.
	duplicate := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationA,
		Name:           "Duplicate Product",
		SKU:            "SKU-DUP-1",
		Price:          2000,
		IsActive:       true,
	}

	err := repository.Create(ctx, duplicate)
	if !errors.Is(err, ErrProductSKUAlreadyExists) {
		t.Fatalf("expected ErrProductSKUAlreadyExists, got %v", err)
	}

	// The raw PostgreSQL error must not leak past the repository.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		t.Fatalf("expected the raw PgError to be translated away, but found one: %v", pgErr)
	}

	// Same SKU, different organisation: must succeed — uniqueness is
	// scoped per organisation, not global.
	otherOrg := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationB,
		Name:           "Same SKU, Different Org",
		SKU:            "SKU-DUP-1",
		Price:          3000,
		IsActive:       true,
	}

	if err := repository.Create(ctx, otherOrg); err != nil {
		t.Fatalf("expected same SKU to succeed under a different organisation, got %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM products WHERE id = $1", otherOrg.ID)
	})
}
