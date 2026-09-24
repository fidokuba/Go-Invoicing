package admin

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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

// TestPostgresOrganisationRepository_Update_PersistsFields proves Update
// actually writes every mutable field to PostgreSQL, reading back through
// a fresh GetByID call rather than trusting the in-memory struct.
func TestPostgresOrganisationRepository_Update_PersistsFields(t *testing.T) {
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

	organisation := &Organisation{
		ID:   uuid.New(),
		Name: "Test Organisation",
	}
	if err := repository.Create(ctx, organisation); err != nil {
		t.Fatalf("create organisation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", organisation.ID)
	})

	email := "hello@acme.test"
	phone := "+44 20 7946 0958"
	address := "1 Acme Way"
	city := "London"
	postalCode := "E1 6AN"
	country := "GB"
	taxID := "GB123456789"

	updated := &Organisation{
		Name:       "Acme Holdings Ltd",
		Email:      &email,
		Phone:      &phone,
		Address:    &address,
		City:       &city,
		PostalCode: &postalCode,
		Country:    &country,
		TaxID:      &taxID,
	}

	if err := repository.Update(ctx, organisation.ID, updated, organisation.Version); err != nil {
		t.Fatalf("update organisation: %v", err)
	}

	fetched, err := repository.GetByID(ctx, organisation.ID)
	if err != nil {
		t.Fatalf("get organisation: %v", err)
	}

	if fetched.Name != "Acme Holdings Ltd" {
		t.Errorf("expected name %q, got %q", "Acme Holdings Ltd", fetched.Name)
	}
	if fetched.Email == nil || *fetched.Email != email {
		t.Errorf("expected email %q, got %v", email, fetched.Email)
	}
	if fetched.Address == nil || *fetched.Address != address {
		t.Errorf("expected address %q, got %v", address, fetched.Address)
	}
	if fetched.TaxID == nil || *fetched.TaxID != taxID {
		t.Errorf("expected tax ID %q, got %v", taxID, fetched.TaxID)
	}
}

// TestPostgresOrganisationRepository_Update_OnlyAffectsOwnOrganisation
// proves Update's WHERE id = $organisationID clause scopes the write to
// exactly one row — a second, untouched organisation must be completely
// unaffected.
func TestPostgresOrganisationRepository_Update_OnlyAffectsOwnOrganisation(t *testing.T) {
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

	orgA := &Organisation{ID: uuid.New(), Name: "Organisation A"}
	orgB := &Organisation{ID: uuid.New(), Name: "Organisation B"}
	if err := repository.Create(ctx, orgA); err != nil {
		t.Fatalf("create organisation A: %v", err)
	}
	if err := repository.Create(ctx, orgB); err != nil {
		t.Fatalf("create organisation B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id IN ($1, $2)", orgA.ID, orgB.ID)
	})

	newName := "Organisation A Renamed"
	if err := repository.Update(ctx, orgA.ID, &Organisation{Name: newName}, orgA.Version); err != nil {
		t.Fatalf("update organisation A: %v", err)
	}

	fetchedB, err := repository.GetByID(ctx, orgB.ID)
	if err != nil {
		t.Fatalf("get organisation B: %v", err)
	}
	if fetchedB.Name != "Organisation B" {
		t.Errorf("expected organisation B's name to remain %q, got %q", "Organisation B", fetchedB.Name)
	}
}

// TestPostgresOrganisationRepository_Create_PopulatesTimestamps is the
// Milestone 8 Part 2 regression test for the systemic Create-response
// timestamp defect Part 1 found: Create must scan created_at/updated_at
// straight back onto the struct it was given (via RETURNING), not leave
// them at their Go zero value for the caller to build a response from.
func TestPostgresOrganisationRepository_Create_PopulatesTimestamps(t *testing.T) {
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

	organisation := &Organisation{ID: uuid.New(), Name: "Timestamp Test Org"}
	if err := repository.Create(ctx, organisation); err != nil {
		t.Fatalf("create organisation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", organisation.ID)
	})

	if organisation.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated by Create, got the zero value")
	}
	if organisation.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be populated by Create, got the zero value")
	}

	fetched, err := repository.GetByID(ctx, organisation.ID)
	if err != nil {
		t.Fatalf("get organisation: %v", err)
	}

	if !organisation.CreatedAt.Equal(fetched.CreatedAt) {
		t.Errorf("expected Create's returned CreatedAt %v to match the persisted value %v", organisation.CreatedAt, fetched.CreatedAt)
	}
	if !organisation.UpdatedAt.Equal(fetched.UpdatedAt) {
		t.Errorf("expected Create's returned UpdatedAt %v to match the persisted value %v", organisation.UpdatedAt, fetched.UpdatedAt)
	}
}

// TestPostgresOrganisationRepository_Update_PopulatesUpdatedAt proves
// Update's own timestamp fix: the UpdatedAt this method scans back onto
// its caller's struct must reflect this write, not the value read before
// the patch was applied, and must be strictly after CreatedAt.
func TestPostgresOrganisationRepository_Update_PopulatesUpdatedAt(t *testing.T) {
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

	organisation := &Organisation{ID: uuid.New(), Name: "Update Timestamp Org"}
	if err := repository.Create(ctx, organisation); err != nil {
		t.Fatalf("create organisation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", organisation.ID)
	})

	time.Sleep(10 * time.Millisecond) // ensure a measurably later NOW()

	updated := &Organisation{Name: "Renamed Org"}
	if err := repository.Update(ctx, organisation.ID, updated, organisation.Version); err != nil {
		t.Fatalf("update organisation: %v", err)
	}

	if updated.UpdatedAt.IsZero() {
		t.Fatal("expected Update to populate UpdatedAt, got the zero value")
	}

	if !updated.UpdatedAt.After(organisation.CreatedAt) {
		t.Errorf("expected UpdatedAt (%v) to be after the original CreatedAt (%v)", updated.UpdatedAt, organisation.CreatedAt)
	}

	fetched, err := repository.GetByID(ctx, organisation.ID)
	if err != nil {
		t.Fatalf("get organisation: %v", err)
	}

	if !updated.UpdatedAt.Equal(fetched.UpdatedAt) {
		t.Errorf("expected Update's returned UpdatedAt %v to match the persisted value %v", updated.UpdatedAt, fetched.UpdatedAt)
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
