package product

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// productSortColumns maps List's public, httpx.ParseSortOrder-validated
// sort field names onto actual SQL columns — the one place a client-
// controlled value is allowed to influence an ORDER BY, and only via
// this fixed, explicit lookup, never by interpolating the field name
// itself.
var productSortColumns = map[string]string{
	"name":      "name",
	"sku":       "sku",
	"price":     "price",
	"createdAt": "created_at",
}

// List returns the page of products matching filter, tenant-scoped to
// organisationID, together with the total count of products matching the
// same filters. The WHERE clause is built once and reused for both the
// item query and the COUNT(*) query, so the two can never drift apart.
//
// Search is bound as a single "%term%" parameter — never string-
// concatenated into the query — matched via ILIKE against name and sku.
// '%' and '_' typed into the search term itself remain live ILIKE
// wildcards; this is standard, expected behaviour and is not escaped.
func (r *PostgresProductRepository) List(
	ctx context.Context,
	organisationID uuid.UUID,
	filter ListFilter,
) ([]*Product, int64, error) {
	conditions := []string{"organisation_id = $1", "deleted_at IS NULL"}
	args := []any{organisationID}

	if filter.IsActive != nil {
		args = append(args, *filter.IsActive)
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", len(args)))
	}

	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		placeholder := len(args)
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d OR sku ILIKE $%d)", placeholder, placeholder))
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	sortColumn, ok := productSortColumns[filter.Sort]
	if !ok {
		sortColumn = "name"
	}

	direction := "ASC"
	if strings.EqualFold(filter.Order, "desc") {
		direction = "DESC"
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM products " + whereClause
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	// id ASC is a stable secondary key for name/price/created_at, none of
	// which is guaranteed unique — sku already is (per organisation), but
	// the secondary key is applied uniformly for simplicity.
	itemQuery := fmt.Sprintf(
		`SELECT
			id, organisation_id, name, description, sku, price, category,
			is_active, created_at, updated_at
		FROM products
		%s
		ORDER BY %s %s, id ASC
		LIMIT $%d OFFSET $%d`,
		whereClause, sortColumn, direction, len(args)+1, len(args)+2,
	)

	rows, err := r.db.Query(ctx, itemQuery, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var products []*Product

	for rows.Next() {
		var p Product

		if err := rows.Scan(
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
		); err != nil {
			return nil, 0, fmt.Errorf("scan product: %w", err)
		}

		products = append(products, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read products: %w", err)
	}

	return products, total, nil
}
