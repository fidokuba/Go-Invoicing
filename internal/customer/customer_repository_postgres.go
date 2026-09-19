package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCustomerNotFound is returned when a lookup finds no matching customer
// within the given organisation, as opposed to a genuine database failure.
var ErrCustomerNotFound = errors.New("customer not found")

// dbExecutor is the minimal query surface this repository needs. Both
// *pgxpool.Pool and pgx.Tx implement it, which is what lets these methods
// run unmodified whether they're operating directly on the pool or inside
// a transaction the caller is managing — the repository doesn't know or
// care which. Same pattern as invoice.dbExecutor and
// administration.dbExecutor.
type dbExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresCustomerRepository is the PostgreSQL-backed implementation of
// CustomerRepository. It holds a dbExecutor rather than a concrete pool,
// so the same code runs unmodified whether it's operating directly on the
// pool or inside a transaction WithTx provides.
type PostgresCustomerRepository struct {
	db dbExecutor
}

// NewPostgresCustomerRepository wires an existing pool into a repository.
func NewPostgresCustomerRepository(db *pgxpool.Pool) *PostgresCustomerRepository {
	return &PostgresCustomerRepository{
		db: db,
	}
}

// WithTx returns a repository that runs its operations against tx instead
// of the pool, so a customer's identity fields can be read within the
// same transaction InvoiceService.Send uses to capture its snapshot. It
// does not begin, commit or roll back anything itself.
func (r *PostgresCustomerRepository) WithTx(tx pgx.Tx) CustomerRepository {
	return &PostgresCustomerRepository{
		db: tx,
	}
}

// Create inserts a new customer row. created_at and updated_at are left
// to PostgreSQL's DEFAULT NOW(), and deleted_at stays NULL until the
// customer is soft-deleted — the two DB-generated values are scanned
// straight back onto customer via RETURNING, so a caller building an API
// response directly from this same *Customer gets real timestamps
// without a second SELECT.
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
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(
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
	).Scan(&customer.CreatedAt, &customer.UpdatedAt)
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
