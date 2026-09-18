package admin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newPostgresRegistrationService wires a real RegistrationService against
// real PostgreSQL repositories — the same construction app.go does — for
// tests that need to prove behaviour fakes can't (real atomicity across
// three tables in one transaction).
func newPostgresRegistrationService(db *pgxpool.Pool) *RegistrationService {
	return NewRegistrationService(
		NewPostgresOrganisationRepository(db),
		NewPostgresSettingsRepository(db),
		NewPostgresUserRepository(db),
		db,
	)
}

// TestRegistrationService_Register_PersistsAtomically proves a successful
// registration really writes all three rows to PostgreSQL, not just
// returns them in memory.
func TestRegistrationService_Register_PersistsAtomically(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	service := newPostgresRegistrationService(db)

	result, err := service.Register(ctx, "Acme Ltd", "Alice", "registration-atomic@example.com", registrationPassword)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", result.User.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM settings WHERE organisation_id = $1", result.Organisation.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", result.Organisation.ID)
	})

	organisationRepository := NewPostgresOrganisationRepository(db)
	persistedOrg, err := organisationRepository.GetByID(ctx, result.Organisation.ID)
	if err != nil {
		t.Fatalf("get persisted organisation: %v", err)
	}
	if persistedOrg.Name != "Acme Ltd" {
		t.Errorf("expected persisted organisation name %q, got %q", "Acme Ltd", persistedOrg.Name)
	}

	settingsRepository := NewPostgresSettingsRepository(db)
	persistedSettings, err := settingsRepository.GetByOrganisationID(ctx, result.Organisation.ID)
	if err != nil {
		t.Fatalf("get persisted settings: %v", err)
	}
	if persistedSettings.InvoicePrefix != "INV-" || persistedSettings.Currency != "GBP" {
		t.Errorf("expected default settings, got %+v", persistedSettings)
	}

	userRepository := NewPostgresUserRepository(db)
	persistedUser, err := userRepository.GetByID(ctx, result.Organisation.ID, result.User.ID)
	if err != nil {
		t.Fatalf("get persisted user: %v", err)
	}
	if persistedUser.Role != UserRoleAdmin {
		t.Errorf("expected persisted user role %q, got %q", UserRoleAdmin, persistedUser.Role)
	}
	if persistedUser.Email != "registration-atomic@example.com" {
		t.Errorf("expected persisted email %q, got %q", "registration-atomic@example.com", persistedUser.Email)
	}
}

// TestRegistrationService_Register_RollsBackAtomicallyOnDuplicateEmail
// proves atomicity against real Postgres: when the user INSERT fails
// (a genuine, organically-reachable failure — the email already exists,
// enforced by the real users_email_unique constraint), neither the
// organisation nor its settings row survive the rollback. This is the
// registration-flow counterpart to
// TestInvoiceService_Create_RollsBackAtomicallyOnLineFailure.
func TestRegistrationService_Register_RollsBackAtomicallyOnDuplicateEmail(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	service := newPostgresRegistrationService(db)

	const email = "registration-duplicate@example.com"

	first, err := service.Register(ctx, "First Co", "Alice", email, registrationPassword)
	if err != nil {
		t.Fatalf("first registration: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", first.User.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM settings WHERE organisation_id = $1", first.Organisation.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", first.Organisation.ID)
	})

	_, err = service.Register(ctx, "Second Co", "Alice Again", email, registrationPassword)
	if !errors.Is(err, ErrUserEmailAlreadyExists) {
		t.Fatalf("expected ErrUserEmailAlreadyExists, got %v", err)
	}

	var organisationCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM organisations WHERE name = $1", "Second Co").Scan(&organisationCount); err != nil {
		t.Fatalf("count organisations: %v", err)
	}
	if organisationCount != 0 {
		t.Errorf("expected 0 organisations named %q after rollback, got %d — the organisation row was not rolled back", "Second Co", organisationCount)
	}

	var settingsCount int
	if err := db.QueryRow(
		ctx,
		"SELECT count(*) FROM settings WHERE organisation_id NOT IN (SELECT id FROM organisations)",
	).Scan(&settingsCount); err != nil {
		t.Fatalf("count orphan settings: %v", err)
	}
	if settingsCount != 0 {
		t.Errorf("expected 0 orphan settings rows after rollback, got %d", settingsCount)
	}
}

// TestRegistrationService_Register_ConcurrentSameEmail_ExactlyOneSucceeds
// is the Milestone 4 Part 6 concurrency proof: two goroutines register
// simultaneously with the same globally-unique email, each under its own
// organisation name. PostgreSQL's users_email_unique constraint — not
// any application-level pre-check — is the sole arbiter: exactly one
// registration must succeed, and the loser's entire transaction
// (organisation + settings + user) must roll back, leaving no orphan
// organisation or settings row behind, regardless of which goroutine
// happens to win the race. Mirrors the concurrency-test style already
// used for invoice-number allocation
// (TestInvoiceService_Create_ConcurrentInvoiceCreation) and payment
// overpayment protection.
func TestRegistrationService_Register_ConcurrentSameEmail_ExactlyOneSucceeds(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	service := newPostgresRegistrationService(db)

	const email = "concurrent-registration@example.com"
	const concurrency = 2
	const orgNamePrefix = "Concurrent Registration Org "

	var wg sync.WaitGroup
	results := make([]*RegistrationResult, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := service.Register(ctx, fmt.Sprintf("%s%d", orgNamePrefix, i), "Alice", email, registrationPassword)
			results[i] = result
			errs[i] = err
		}(i)
	}

	wg.Wait()

	var successCount, conflictCount int
	var winner *RegistrationResult

	for i, err := range errs {
		switch {
		case err == nil:
			successCount++
			winner = results[i]
		case errors.Is(err, ErrUserEmailAlreadyExists):
			conflictCount++
		default:
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	if successCount != 1 {
		t.Fatalf("expected exactly 1 successful registration, got %d", successCount)
	}

	if conflictCount != 1 {
		t.Fatalf("expected exactly 1 rejection via ErrUserEmailAlreadyExists, got %d", conflictCount)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", winner.User.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM settings WHERE organisation_id = $1", winner.Organisation.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", winner.Organisation.ID)
	})

	var userCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM users WHERE email = $1", email).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Errorf("expected exactly 1 user to exist for %q, got %d", email, userCount)
	}

	var orgCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM organisations WHERE name LIKE $1", orgNamePrefix+"%").Scan(&orgCount); err != nil {
		t.Fatalf("count organisations: %v", err)
	}
	if orgCount != 1 {
		t.Errorf("expected exactly 1 surviving organisation (the loser's must have rolled back), got %d", orgCount)
	}

	var settingsCount int
	if err := db.QueryRow(
		ctx,
		"SELECT count(*) FROM settings WHERE organisation_id NOT IN (SELECT id FROM organisations)",
	).Scan(&settingsCount); err != nil {
		t.Fatalf("count orphan settings: %v", err)
	}
	if settingsCount != 0 {
		t.Errorf("expected 0 orphan settings rows, got %d", settingsCount)
	}
}
