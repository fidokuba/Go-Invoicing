package admin

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresOrganisationRepository_CreateAndGetByID(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx := context.Background()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	// Registered via t.Cleanup (not a bare defer) so this runs after the
	// row-delete cleanup below: t.Cleanup callbacks fire in last-added,
	// first-called order, all of them after the test function's own
	// defers have already run. A plain "defer db.Close()" here would close
	// the pool before a later-registered t.Cleanup got a chance to use it.
	t.Cleanup(func() { db.Close() })

	repository := NewPostgresOrganisationRepository(db)

	organisation := &Organisation{
		ID:   uuid.New(),
		Name: "Test Organisation",
	}

	err = repository.Create(ctx, organisation)
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM organisations WHERE id = $1",
			organisation.ID,
		)
	})

	created, err := repository.GetByID(ctx, organisation.ID)
	if err != nil {
		t.Fatalf("get organisation: %v", err)
	}

	if created.ID != organisation.ID {
		t.Errorf("expected ID %v, got %v", organisation.ID, created.ID)
	}

	if created.Name != organisation.Name {
		t.Errorf("expected name %q, got %q", organisation.Name, created.Name)
	}
}

func TestPostgresOrganisationRepository_GetByID_NotFound(t *testing.T) {
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

	repository := NewPostgresOrganisationRepository(db)

	organisation, err := repository.GetByID(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, ErrOrganisationNotFound) {
		t.Fatalf("expected ErrOrganisationNotFound, got %v", err)
	}

	if organisation != nil {
		t.Errorf("expected nil organisation, got %+v", organisation)
	}
}
