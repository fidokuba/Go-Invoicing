package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCustomerNotFound is returned when a lookup finds no matching customer
// within the given organisation, as opposed to a genuine database failure.
var ErrCustomerNotFound = errors.New("customer not found")

// PostgresCustomerRepository is the PostgreSQL-backed implementation of
// CustomerRepository. It holds a connection pool rather than creating one
// itself, so the caller decides how the pool is configured and when it is
// closed.
type PostgresCustomerRepository struct {
	db *pgxpool.Pool
}

// NewPostgresCustomerRepository wires an existing pool into a repository.
func NewPostgresCustomerRepository(db *pgxpool.Pool) *PostgresCustomerRepository {
	return &PostgresCustomerRepository{
		db: db,
	}
}

// Create inserts a new customer row. created_at and updated_at are left to
// PostgreSQL's DEFAULT NOW(), and deleted_at stays NULL until the customer
// is soft-deleted.
func (r *PostgresCustomerRepository) Create(
	ctx context.Context,
	customer *Customer,
) error {
	const query = `
		INSERT INTO customers (
			id,
			organisation_id,
			name,
			email,
			phone,
			company_name,
			tax_id,
			status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err := r.db.Exec(
		ctx,
		query,
		customer.ID,
		customer.OrganisationID,
		customer.Name,
		customer.Email,
		customer.Phone,
		customer.CompanyName,
		customer.TaxID,
		customer.Status,
	)
	if err != nil {
		return fmt.Errorf("create customer: %w", err)
	}

	return nil
}

// GetByID fetches a single customer by its primary key, scoped to the
// supplied organisation so one organisation can never read another's
// customer. Soft-deleted customers are excluded.
func (r *PostgresCustomerRepository) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
) (*Customer, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			name,
			email,
			phone,
			company_name,
			tax_id,
			status,
			created_at,
			updated_at,
			deleted_at
		FROM customers
		WHERE organisation_id = $1
			AND id = $2
			AND deleted_at IS NULL
	`

	var c Customer

	err := r.db.QueryRow(
		ctx,
		query,
		organisationID,
		customerID,
	).Scan(
		&c.ID,
		&c.OrganisationID,
		&c.Name,
		&c.Email,
		&c.Phone,
		&c.CompanyName,
		&c.TaxID,
		&c.Status,
		&c.CreatedAt,
		&c.UpdatedAt,
		&c.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCustomerNotFound
		}

		return nil, fmt.Errorf("get customer by id: %w", err)
	}

	return &c, nil
}
