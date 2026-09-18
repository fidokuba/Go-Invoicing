package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresAddressRepository is the PostgreSQL-backed implementation of
// AddressRepository. It holds a connection pool rather than creating one
// itself, so the caller decides how the pool is configured and when it is
// closed. Unlike InvoiceRepository/PaymentRepository, it has no WithTx: a
// billing-address upsert is already a single atomic statement, and
// nothing in this milestone needs it to participate in a wider
// transaction.
type PostgresAddressRepository struct {
	db *pgxpool.Pool
}

// NewPostgresAddressRepository wires an existing pool into a repository.
func NewPostgresAddressRepository(db *pgxpool.Pool) *PostgresAddressRepository {
	return &PostgresAddressRepository{
		db: db,
	}
}

// GetBillingAddressByCustomerID joins back to customers so organisationID
// is enforced by the query itself (addresses has no organisation_id
// column of its own), the same defense-in-depth pattern
// payment_repository_postgres.go already uses for payments against
// invoices. A soft-deleted customer, a customer belonging to another
// organisation, and a genuinely address-less customer are all
// indistinguishable here — every one of them yields zero rows and
// ErrBillingAddressNotFound.
func (r *PostgresAddressRepository) GetBillingAddressByCustomerID(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
) (*Address, error) {
	const query = `
		SELECT
			a.id,
			a.customer_id,
			a.type,
			a.street,
			a.city,
			a.state,
			a.postal_code,
			a.country,
			a.is_default,
			a.created_at,
			a.updated_at
		FROM addresses a
		JOIN customers c ON c.id = a.customer_id
		WHERE a.customer_id = $1
			AND c.organisation_id = $2
			AND a.type = $3
			AND c.deleted_at IS NULL
	`

	var a Address

	err := r.db.QueryRow(ctx, query, customerID, organisationID, AddressTypeBilling).Scan(
		&a.ID,
		&a.CustomerID,
		&a.Type,
		&a.Street,
		&a.City,
		&a.State,
		&a.PostalCode,
		&a.Country,
		&a.IsDefault,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBillingAddressNotFound
		}

		return nil, fmt.Errorf("get billing address: %w", err)
	}

	return &a, nil
}

// UpsertBillingAddress inserts or replaces customerID's billing address in
// one statement. The INSERT's source is a SELECT against customers rather
// than a plain VALUES list — exactly like PaymentRepository.Create's
// INSERT ... SELECT ... FROM invoices — so a row is only ever written at
// all if organisationID and customerID together match a real, tenant-
// owned, non-deleted customer; zero rows in means zero rows out, scanned
// as pgx.ErrNoRows below.
//
// ON CONFLICT (customer_id) WHERE type = 'billing' targets the partial
// unique index the migration adds specifically for this: if a billing
// address for this customer already exists, its Street/City/State/
// PostalCode/Country are replaced in place (same id, same row) rather
// than a second row being inserted — this is what makes PUT a genuine
// upsert instead of an ever-growing history of billing addresses.
func (r *PostgresAddressRepository) UpsertBillingAddress(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
	address *Address,
) (*Address, error) {
	const query = `
		INSERT INTO addresses (
			id,
			customer_id,
			type,
			street,
			city,
			state,
			postal_code,
			country
		)
		SELECT
			$1, c.id, $3, $4, $5, $6, $7, $8
		FROM customers c
		WHERE c.id = $2
			AND c.organisation_id = $9
			AND c.deleted_at IS NULL
		ON CONFLICT (customer_id) WHERE type = 'billing'
		DO UPDATE SET
			street      = EXCLUDED.street,
			city        = EXCLUDED.city,
			state       = EXCLUDED.state,
			postal_code = EXCLUDED.postal_code,
			country     = EXCLUDED.country,
			updated_at  = NOW()
		RETURNING id, customer_id, type, street, city, state, postal_code, country, is_default, created_at, updated_at
	`

	var a Address

	err := r.db.QueryRow(
		ctx,
		query,
		address.ID,
		customerID,
		AddressTypeBilling,
		address.Street,
		address.City,
		address.State,
		address.PostalCode,
		address.Country,
		organisationID,
	).Scan(
		&a.ID,
		&a.CustomerID,
		&a.Type,
		&a.Street,
		&a.City,
		&a.State,
		&a.PostalCode,
		&a.Country,
		&a.IsDefault,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBillingAddressNotFound
		}

		return nil, fmt.Errorf("upsert billing address: %w", err)
	}

	return &a, nil
}
