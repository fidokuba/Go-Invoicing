package admin

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Milestone 13 Part 2: the repositories' atomic version predicate against
// real PostgreSQL — the authoritative optimistic-concurrency guarantee.

func storedVersion(t *testing.T, db *pgxpool.Pool, table string, idColumn string, id uuid.UUID) int64 {
	t.Helper()

	var version int64
	// table/idColumn are fixed literals from this file, never input.
	if err := db.QueryRow(context.Background(), "SELECT version FROM "+table+" WHERE "+idColumn+" = $1", id).Scan(&version); err != nil {
		t.Fatalf("read %s version: %v", table, err)
	}

	return version
}

func TestOrganisationRepository_Update_IncrementsVersion(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	repository := NewPostgresOrganisationRepository(db)
	organisationID := createTestOrganisation(t, db)

	if got := storedVersion(t, db, "organisations", "id", organisationID); got != 1 {
		t.Fatalf("expected a new organisation to start at version 1, got %d", got)
	}

	for want := int64(2); want <= 3; want++ {
		organisation := &Organisation{Name: "Renamed"}
		if err := repository.Update(ctx, organisationID, organisation, want-1); err != nil {
			t.Fatalf("update at version %d: %v", want-1, err)
		}
		if organisation.Version != want {
			t.Errorf("expected Update to return version %d, got %d", want, organisation.Version)
		}
		if got := storedVersion(t, db, "organisations", "id", organisationID); got != want {
			t.Errorf("expected stored version %d, got %d", want, got)
		}
	}
}

func TestOrganisationRepository_Update_StaleVersionWritesNothing(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	repository := NewPostgresOrganisationRepository(db)
	organisationID := createTestOrganisation(t, db)

	if err := repository.Update(ctx, organisationID, &Organisation{Name: "First writer"}, 1); err != nil {
		t.Fatalf("first update: %v", err)
	}

	err := repository.Update(ctx, organisationID, &Organisation{Name: "Stale writer"}, 1)
	if !errors.Is(err, ErrOrganisationVersionConflict) {
		t.Fatalf("expected ErrOrganisationVersionConflict, got %v", err)
	}

	current, err := repository.GetByID(ctx, organisationID)
	if err != nil {
		t.Fatalf("get organisation: %v", err)
	}
	if current.Name != "First writer" || current.Version != 2 {
		t.Errorf("expected the first writer's change at version 2 to survive, got %q at version %d", current.Name, current.Version)
	}

	if err := repository.Update(ctx, uuid.New(), &Organisation{Name: "Nobody"}, 1); !errors.Is(err, ErrOrganisationNotFound) {
		t.Errorf("expected ErrOrganisationNotFound for a missing organisation, got %v", err)
	}
}

func TestSettingsRepository_Update_IncrementsVersionAndRejectsStale(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	repository := NewPostgresSettingsRepository(db)
	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID)

	updated := &Settings{InvoicePrefix: "FIRST-", Currency: "EUR", PaymentTerms: 14}
	if err := repository.Update(ctx, organisationID, updated, 1); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("expected version 2, got %d", updated.Version)
	}

	err := repository.Update(ctx, organisationID, &Settings{InvoicePrefix: "STALE-", Currency: "USD", PaymentTerms: 7}, 1)
	if !errors.Is(err, ErrSettingsVersionConflict) {
		t.Fatalf("expected ErrSettingsVersionConflict, got %v", err)
	}

	current, err := repository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if current.InvoicePrefix != "FIRST-" || current.Currency != "EUR" || current.Version != 2 {
		t.Errorf("expected the first writer's settings at version 2 to survive, got %+v", current)
	}
}

// Allocating an invoice number is an internal write, not a user-facing
// modification of the settings resource: it must never bump version (or
// every invoice created would make an admin's open settings form stale).
func TestSettingsRepository_UpdateInvoiceNumber_DoesNotChangeVersion(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	repository := NewPostgresSettingsRepository(db)
	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID)

	for n := 1; n <= 3; n++ {
		if err := repository.UpdateInvoiceNumber(ctx, organisationID, n); err != nil {
			t.Fatalf("allocate invoice number %d: %v", n, err)
		}
	}

	settings, err := repository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if settings.InvoiceNumber != 3 || settings.Version != 1 {
		t.Errorf("expected invoice number 3 at version 1, got %d at version %d", settings.InvoiceNumber, settings.Version)
	}

	// And an admin holding version 1 can still save.
	if err := repository.Update(ctx, organisationID, &Settings{InvoicePrefix: "INV-", Currency: "GBP", PaymentTerms: 30}, 1); err != nil {
		t.Errorf("expected an update at version 1 to succeed after number allocation, got %v", err)
	}
}

// Many writers holding the same version race; the SQL predicate lets
// exactly one win. Released together by a barrier; the assertions hold
// under any interleaving, so nothing here depends on timing.
func TestOptimisticConcurrency_ConcurrentSameVersionOnlyOneSucceeds(t *testing.T) {
	db := newTestPool(t)
	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID)
	organisations := NewPostgresOrganisationRepository(db)
	settings := NewPostgresSettingsRepository(db)

	const writers = 10

	for _, tc := range []struct {
		name     string
		update   func(i int) error
		conflict error
		version  func() int64
	}{
		{
			name: "organisation",
			update: func(i int) error {
				return organisations.Update(context.Background(), organisationID, &Organisation{Name: "Writer " + uuid.NewString()}, 1)
			},
			conflict: ErrOrganisationVersionConflict,
			version:  func() int64 { return storedVersion(t, db, "organisations", "id", organisationID) },
		},
		{
			name: "settings",
			update: func(i int) error {
				return settings.Update(context.Background(), organisationID, &Settings{InvoicePrefix: "W" + uuid.NewString()[:4] + "-", Currency: "GBP", PaymentTerms: i}, 1)
			},
			conflict: ErrSettingsVersionConflict,
			version:  func() int64 { return storedVersion(t, db, "settings", "organisation_id", organisationID) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := make([]error, writers)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < writers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					errs[i] = tc.update(i)
				}(i)
			}
			close(start)
			wg.Wait()

			succeeded := 0
			for i, err := range errs {
				switch {
				case err == nil:
					succeeded++
				case errors.Is(err, tc.conflict):
				default:
					t.Fatalf("writer %d: unexpected error %v", i, err)
				}
			}

			if succeeded != 1 {
				t.Errorf("expected exactly 1 writer to succeed, got %d", succeeded)
			}
			if got := tc.version(); got != 2 {
				t.Errorf("expected version 2 after exactly one successful write, got %d", got)
			}
		})
	}
}
