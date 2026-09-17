package customer

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
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

func TestPostgresCustomerRepository_CreateAndGetByID(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx := context.Background()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	// t.Cleanup (not defer) so this runs after row-delete cleanups below:
	// Cleanup callbacks fire last-added-first, all after the test
	// function's own defers have already run.
	t.Cleanup(func() { db.Close() })

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresCustomerRepository(db)

	c := &Customer{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Test Customer",
		Status:         "active",
	}

	err = repository.Create(ctx, c)
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM customers WHERE id = $1",
			c.ID,
		)
	})

	created, err := repository.GetByID(ctx, organisationID, c.ID)
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}

	if created.ID != c.ID {
		t.Errorf("expected ID %v, got %v", c.ID, created.ID)
	}

	if created.Name != c.Name {
		t.Errorf("expected name %q, got %q", c.Name, created.Name)
	}

	if created.OrganisationID != organisationID {
		t.Errorf("expected organisation ID %v, got %v", organisationID, created.OrganisationID)
	}
}

func TestPostgresCustomerRepository_GetByID_NotFound(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx := context.Background()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	// t.Cleanup (not defer) so this runs after row-delete cleanups below:
	// Cleanup callbacks fire last-added-first, all after the test
	// function's own defers have already run.
	t.Cleanup(func() { db.Close() })

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresCustomerRepository(db)

	c, err := repository.GetByID(ctx, organisationID, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("expected ErrCustomerNotFound, got %v", err)
	}

	if c != nil {
		t.Errorf("expected nil customer, got %+v", c)
	}
}

// TestPostgresCustomerRepository_GetByID_OrganisationScoping proves that a
// customer belonging to one organisation cannot be retrieved through a
// different organisation's ID.
func TestPostgresCustomerRepository_GetByID_OrganisationScoping(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx := context.Background()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	// t.Cleanup (not defer) so this runs after row-delete cleanups below:
	// Cleanup callbacks fire last-added-first, all after the test
	// function's own defers have already run.
	t.Cleanup(func() { db.Close() })

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	repository := NewPostgresCustomerRepository(db)

	c := &Customer{
		ID:             uuid.New(),
		OrganisationID: organisationA,
		Name:           "Test Customer",
		Status:         "active",
	}

	if err := repository.Create(ctx, c); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM customers WHERE id = $1",
			c.ID,
		)
	})

	// Fetching through the owning organisation works.
	if _, err := repository.GetByID(ctx, organisationA, c.ID); err != nil {
		t.Fatalf("get customer via owning organisation: %v", err)
	}

	// Fetching the same customer ID through a different organisation must
	// behave exactly like a not-found lookup.
	result, err := repository.GetByID(ctx, organisationB, c.ID)
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("expected ErrCustomerNotFound when scoped to the wrong organisation, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil customer, got %+v", result)
	}
}
