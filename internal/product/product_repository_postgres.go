package product

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrProductNotFound is returned when a lookup finds no matching product
// within the given organisation, as opposed to a genuine database failure.
var ErrProductNotFound = errors.New("product not found")

// ErrProductSKUAlreadyExists is returned when Create violates the
// products_organisation_sku_unique constraint, as opposed to a genuine
// database failure.
var ErrProductSKUAlreadyExists = errors.New("product SKU already exists for this organisation")

// postgresUniqueViolation is the PostgreSQL SQLSTATE code for a unique
// constraint violation.
const postgresUniqueViolation = "23505"

// PostgresProductRepository is the PostgreSQL-backed implementation of
// ProductRepository. It holds a connection pool rather than creating one
// itself, so the caller decides how the pool is configured and when it is
// closed.
type PostgresProductRepository struct {
	db *pgxpool.Pool
}

// NewPostgresProductRepository wires an existing pool into a repository.
func NewPostgresProductRepository(db *pgxpool.Pool) *PostgresProductRepository {
	return &PostgresProductRepository{
		db: db,
	}
}

// Create inserts a new product row. created_at and updated_at are left to
// PostgreSQL's DEFAULT NOW(), and deleted_at stays NULL until the product
// is soft-deleted — the two DB-generated values are scanned straight
// back onto product via RETURNING, so a caller building an API response
// directly from this same *Product gets real timestamps without a
// second SELECT.
//
// SKU uniqueness within an organisation is enforced by the
// products_organisation_sku_unique database constraint, not by a
// pre-emptive lookup here. A violation of that constraint is translated
// into ErrProductSKUAlreadyExists; the raw PostgreSQL error never reaches
// callers for this case.
func (r *PostgresProductRepository) Create(
	ctx context.Context,
	product *Product,
) error {
	const query = `
		INSERT INTO products (
			id,
			organisation_id,
			name,
			description,
			sku,
			price,
			category,
			is_active
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		product.ID,
		product.OrganisationID,
		product.Name,
		product.Description,
		product.SKU,
		product.Price,
		product.Category,
		product.IsActive,
	).Scan(&product.CreatedAt, &product.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation {
			return ErrProductSKUAlreadyExists
		}

		return fmt.Errorf("create product: %w", err)
	}

	return nil
}

// GetByID fetches a single product by its primary key, scoped to the
// supplied organisation so one organisation can never read another's
// product. Soft-deleted products are excluded.
func (r *PostgresProductRepository) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	productID uuid.UUID,
) (*Product, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			name,
			description,
			sku,
			price,
			category,
			is_active,
			created_at,
			updated_at,
			deleted_at
		FROM products
		WHERE organisation_id = $1
			AND id = $2
			AND deleted_at IS NULL
	`

	var p Product

	err := r.db.QueryRow(
		ctx,
		query,
		organisationID,
		productID,
	).Scan(
		&p.ID,
		&p.OrganisationID,
		&p.Name,
		&p.Description,
		&p.SKU,
		&p.Price,
		&p.Category,
		&p.IsActive,
		&p.CreatedAt,
		&p.UpdatedAt,
		&p.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}

		return nil, fmt.Errorf("get product by id: %w", err)
	}

	return &p, nil
}
