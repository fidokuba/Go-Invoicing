package customer

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
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
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

// customerSortColumns maps List's public, httpx.ParseSortOrder-validated
// sort field names onto actual SQL columns — the one place a client-
// controlled value is allowed to influence an ORDER BY, and only via
// this fixed, explicit lookup, never by interpolating the field name
// itself.
var customerSortColumns = map[string]string{
	"name":        "name",
	"companyName": "company_name",
	"createdAt":   "created_at",
}

// List returns the page of customers matching filter, tenant-scoped to
// organisationID, together with the total count of customers matching
// the same filters. The WHERE clause is built once (whereClause/args) and
// reused for both the item query and the COUNT(*) query, so the two can
// never drift apart — see CustomerService.List for how Filter.Status is
// validated and Filter.Sort/Order are resolved before ever reaching here.
//
// Search is bound as a single "%term%" parameter — never string-
// concatenated into the query — matched via ILIKE against name,
// company_name and email. '%' and '_' typed into the search term itself
// remain live ILIKE wildcards (e.g. searching "50%" matches literally
// anything after "50"); this is standard, expected ILIKE behaviour and
// is not escaped.
func (r *PostgresCustomerRepository) List(
	ctx context.Context,
	organisationID uuid.UUID,
	filter ListFilter,
) ([]*Customer, int64, error) {
	conditions := []string{"organisation_id = $1", "deleted_at IS NULL"}
	args := []any{organisationID}

	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}

	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		placeholder := len(args)
		conditions = append(conditions, fmt.Sprintf(
			"(name ILIKE $%d OR company_name ILIKE $%d OR email ILIKE $%d)",
			placeholder, placeholder, placeholder,
		))
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	sortColumn, ok := customerSortColumns[filter.Sort]
	if !ok {
		sortColumn = "name"
	}

	direction := "ASC"
	if strings.EqualFold(filter.Order, "desc") {
		direction = "DESC"
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM customers " + whereClause
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customers: %w", err)
	}

	// id ASC is a stable secondary key for every sort column above, none
	// of which is guaranteed unique (unlike, say, products.sku).
	itemQuery := fmt.Sprintf(
		`SELECT
			id, organisation_id, name, email, phone, company_name, tax_id,
			status, created_at, updated_at
		FROM customers
		%s
		ORDER BY %s %s, id ASC
		LIMIT $%d OFFSET $%d`,
		whereClause, sortColumn, direction, len(args)+1, len(args)+2,
	)

	rows, err := r.db.Query(ctx, itemQuery, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()

	var customers []*Customer

	for rows.Next() {
		var c Customer

		if err := rows.Scan(
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
		); err != nil {
			return nil, 0, fmt.Errorf("scan customer: %w", err)
		}

		customers = append(customers, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read customers: %w", err)
	}

	return customers, total, nil
}
