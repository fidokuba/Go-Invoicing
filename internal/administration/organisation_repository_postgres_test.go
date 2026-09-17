package admin

import (
	"context"
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
	defer db.Close()

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
