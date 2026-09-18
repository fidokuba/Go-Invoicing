package customer

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createTestCustomer inserts a minimal customer row directly and
// registers cleanup for it.
func createTestCustomer(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) uuid.UUID {
	t.Helper()

	customerID := uuid.New()

	_, err := db.Exec(
		context.Background(),
		"INSERT INTO customers (id, organisation_id, name, status) VALUES ($1, $2, $3, $4)",
		customerID,
		organisationID,
		"Test Customer",
		"active",
	)
	if err != nil {
		t.Fatalf("create test customer: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM addresses WHERE customer_id = $1", customerID)
		_, _ = db.Exec(context.Background(), "DELETE FROM customers WHERE id = $1", customerID)
	})

	return customerID
}

func newAddressTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func TestPostgresAddressRepository_UpsertBillingAddress_CreatesFirst(t *testing.T) {
	db := newAddressTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresAddressRepository(db)

	address := &Address{
		ID:         uuid.New(),
		Street:     "1 Acme Way",
		City:       "London",
		State:      "",
		PostalCode: "E1 6AN",
		Country:    "GB",
	}

	created, err := repository.UpsertBillingAddress(ctx, organisationID, customerID, address)
	if err != nil {
		t.Fatalf("upsert billing address: %v", err)
	}

	if created.CustomerID != customerID {
		t.Errorf("expected customer ID %v, got %v", customerID, created.CustomerID)
	}

	if created.Type != AddressTypeBilling {
		t.Errorf("expected type %q, got %q", AddressTypeBilling, created.Type)
	}

	if created.Street != "1 Acme Way" || created.City != "London" || created.PostalCode != "E1 6AN" || created.Country != "GB" {
		t.Errorf("unexpected address fields: %+v", created)
	}

	fetched, err := repository.GetBillingAddressByCustomerID(ctx, organisationID, customerID)
	if err != nil {
		t.Fatalf("get billing address: %v", err)
	}

	if fetched.ID != created.ID {
		t.Errorf("expected GET to return the same address ID %v, got %v", created.ID, fetched.ID)
	}
}

// TestPostgresAddressRepository_UpsertBillingAddress_SecondCallUpdatesNotDuplicates
// is the real-Postgres proof of the partial unique index doing its job: a
// second upsert for the same customer replaces the row in place rather
// than violating the constraint or creating a second row.
func TestPostgresAddressRepository_UpsertBillingAddress_SecondCallUpdatesNotDuplicates(t *testing.T) {
	db := newAddressTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresAddressRepository(db)

	first, err := repository.UpsertBillingAddress(ctx, organisationID, customerID, &Address{
		ID:         uuid.New(),
		Street:     "1 Acme Way",
		City:       "London",
		PostalCode: "E1 6AN",
		Country:    "GB",
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := repository.UpsertBillingAddress(ctx, organisationID, customerID, &Address{
		ID:         uuid.New(), // deliberately a different ID — must be ignored on conflict
		Street:     "2 New Street",
		City:       "Manchester",
		PostalCode: "M1 1AE",
		Country:    "GB",
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("expected the same row (ID %v) to be updated in place, got a different ID %v", first.ID, second.ID)
	}

	if second.Street != "2 New Street" || second.City != "Manchester" {
		t.Errorf("expected the second upsert's fields to take effect, got %+v", second)
	}

	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM addresses WHERE customer_id = $1 AND type = 'billing'", customerID).Scan(&count); err != nil {
		t.Fatalf("count billing addresses: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 billing address row after two upserts, got %d", count)
	}
}

func TestPostgresAddressRepository_GetBillingAddressByCustomerID_NoneYet(t *testing.T) {
	db := newAddressTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresAddressRepository(db)

	address, err := repository.GetBillingAddressByCustomerID(ctx, organisationID, customerID)
	if !errors.Is(err, ErrBillingAddressNotFound) {
		t.Fatalf("expected ErrBillingAddressNotFound, got %v", err)
	}
	if address != nil {
		t.Errorf("expected nil address, got %+v", address)
	}
}

// TestPostgresAddressRepository_GetBillingAddressByCustomerID_OrganisationScoping
// proves a billing address cannot be retrieved through a different
// organisation's ID, even for a genuinely-existing address.
func TestPostgresAddressRepository_GetBillingAddressByCustomerID_OrganisationScoping(t *testing.T) {
	db := newAddressTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)
	repository := NewPostgresAddressRepository(db)

	if _, err := repository.UpsertBillingAddress(ctx, organisationA, customerID, &Address{
		ID:         uuid.New(),
		Street:     "1 Acme Way",
		City:       "London",
		PostalCode: "E1 6AN",
		Country:    "GB",
	}); err != nil {
		t.Fatalf("upsert billing address: %v", err)
	}

	if _, err := repository.GetBillingAddressByCustomerID(ctx, organisationA, customerID); err != nil {
		t.Fatalf("get via owning organisation: %v", err)
	}

	result, err := repository.GetBillingAddressByCustomerID(ctx, organisationB, customerID)
	if !errors.Is(err, ErrBillingAddressNotFound) {
		t.Fatalf("expected ErrBillingAddressNotFound when scoped to the wrong organisation, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil address, got %+v", result)
	}
}

// TestPostgresAddressRepository_UpsertBillingAddress_OrganisationScoping
// proves a caller cannot create or overwrite another organisation's
// customer's billing address by supplying the right customer ID with the
// wrong organisation ID — the INSERT ... SELECT ... FROM customers WHERE
// ... AND organisation_id = $X predicate matches zero rows, so nothing is
// written.
func TestPostgresAddressRepository_UpsertBillingAddress_OrganisationScoping(t *testing.T) {
	db := newAddressTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)
	repository := NewPostgresAddressRepository(db)

	result, err := repository.UpsertBillingAddress(ctx, organisationB, customerID, &Address{
		ID:         uuid.New(),
		Street:     "Attacker Street",
		City:       "Nowhere",
		PostalCode: "00000",
		Country:    "XX",
	})
	if !errors.Is(err, ErrBillingAddressNotFound) {
		t.Fatalf("expected ErrBillingAddressNotFound, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil address, got %+v", result)
	}

	// Confirm nothing was written for the real owner either.
	if _, err := repository.GetBillingAddressByCustomerID(ctx, organisationA, customerID); !errors.Is(err, ErrBillingAddressNotFound) {
		t.Fatalf("expected the real owner to still have no billing address, got %v", err)
	}

	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM addresses WHERE customer_id = $1", customerID).Scan(&count); err != nil {
		t.Fatalf("count addresses: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 address rows after a cross-tenant upsert attempt, got %d", count)
	}
}

// TestPostgresAddressRepository_UpsertBillingAddress_SoftDeletedCustomer
// proves a soft-deleted customer's billing address cannot be created —
// the same deleted_at IS NULL predicate GetByID already uses for
// customers themselves.
func TestPostgresAddressRepository_UpsertBillingAddress_SoftDeletedCustomer(t *testing.T) {
	db := newAddressTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresAddressRepository(db)

	if _, err := db.Exec(ctx, "UPDATE customers SET deleted_at = NOW() WHERE id = $1", customerID); err != nil {
		t.Fatalf("soft-delete customer: %v", err)
	}

	_, err := repository.UpsertBillingAddress(ctx, organisationID, customerID, &Address{
		ID:         uuid.New(),
		Street:     "1 Acme Way",
		City:       "London",
		PostalCode: "E1 6AN",
		Country:    "GB",
	})
	if !errors.Is(err, ErrBillingAddressNotFound) {
		t.Fatalf("expected ErrBillingAddressNotFound for a soft-deleted customer, got %v", err)
	}
}
