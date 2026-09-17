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

// createTestOrganisation inserts a minimal organisation row directly (not
// via OrganisationService, to avoid pulling its settings-provisioning
// side effect into these repository-focused tests) and registers cleanup
// for it.
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
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", organisationID)
	})

	return organisationID
}

func createTestSettings(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) *Settings {
	t.Helper()

	repository := NewPostgresSettingsRepository(db)
	settings := &Settings{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		InvoicePrefix:  "INV-",
		InvoiceNumber:  0,
		Currency:       "GBP",
		PaymentTerms:   30,
	}

	if err := repository.Create(context.Background(), settings); err != nil {
		t.Fatalf("create test settings: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM settings WHERE id = $1", settings.ID)
	})

	return settings
}

func TestPostgresSettingsRepository_CreateAndGetByOrganisationID(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	settings := createTestSettings(t, db, organisationID)

	repository := NewPostgresSettingsRepository(db)

	got, err := repository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	if got.ID != settings.ID {
		t.Errorf("expected ID %v, got %v", settings.ID, got.ID)
	}

	if got.InvoicePrefix != "INV-" {
		t.Errorf("expected prefix %q, got %q", "INV-", got.InvoicePrefix)
	}

	if got.InvoiceNumber != 0 {
		t.Errorf("expected invoice number 0, got %d", got.InvoiceNumber)
	}

	if got.Currency != "GBP" {
		t.Errorf("expected currency %q, got %q", "GBP", got.Currency)
	}

	if got.PaymentTerms != 30 {
		t.Errorf("expected payment terms 30, got %d", got.PaymentTerms)
	}
}

func TestPostgresSettingsRepository_GetByOrganisationID_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresSettingsRepository(db)

	got, err := repository.GetByOrganisationID(ctx, uuid.New())
	if !errors.Is(err, ErrSettingsNotFound) {
		t.Fatalf("expected ErrSettingsNotFound, got %v", err)
	}

	if got != nil {
		t.Errorf("expected nil settings, got %+v", got)
	}
}

func TestPostgresSettingsRepository_UpdateInvoiceNumber(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID)

	repository := NewPostgresSettingsRepository(db)

	if err := repository.UpdateInvoiceNumber(ctx, organisationID, 5); err != nil {
		t.Fatalf("update invoice number: %v", err)
	}

	got, err := repository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	if got.InvoiceNumber != 5 {
		t.Errorf("expected invoice number 5, got %d", got.InvoiceNumber)
	}
}

func TestPostgresSettingsRepository_UpdateInvoiceNumber_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresSettingsRepository(db)

	err := repository.UpdateInvoiceNumber(ctx, uuid.New(), 5)
	if !errors.Is(err, ErrSettingsNotFound) {
		t.Fatalf("expected ErrSettingsNotFound, got %v", err)
	}
}

// TestPostgresSettingsRepository_GetForUpdate_LocksRow proves GetForUpdate
// actually takes a row lock, rather than just returning success: a second
// transaction's GetForUpdate on the same organisation must block until the
// first transaction finishes, and only then proceed.
func TestPostgresSettingsRepository_GetForUpdate_LocksRow(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID)

	repository := NewPostgresSettingsRepository(db)

	tx1, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}

	if _, err := repository.WithTx(tx1).GetForUpdate(ctx, organisationID); err != nil {
		t.Fatalf("tx1 GetForUpdate: %v", err)
	}

	acquired := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		defer close(done)

		tx2, err := db.Begin(context.Background())
		if err != nil {
			done <- err
			return
		}
		defer tx2.Rollback(context.Background())

		if _, err := repository.WithTx(tx2).GetForUpdate(context.Background(), organisationID); err != nil {
			done <- err
			return
		}

		close(acquired)
	}()

	// tx1 holds the lock: tx2's GetForUpdate must still be blocked after a
	// short wait.
	select {
	case <-acquired:
		t.Fatal("expected tx2's GetForUpdate to block while tx1 holds the lock, but it returned immediately")
	case err := <-done:
		t.Fatalf("tx2 goroutine finished unexpectedly early: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: tx2 is still waiting on the lock.
	}

	if err := tx1.Rollback(ctx); err != nil {
		t.Fatalf("rollback tx1: %v", err)
	}

	// Releasing tx1's lock must let tx2 proceed.
	select {
	case <-acquired:
		// tx2 acquired the lock after tx1 released it, as expected.
	case err := <-done:
		t.Fatalf("tx2 failed instead of acquiring the lock: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("expected tx2 to unblock after tx1 rolled back, but it did not")
	}

	// Wait for the goroutine to fully finish (it still rolls back tx2)
	// before the test returns, so nothing outlives this test.
	<-done
}
