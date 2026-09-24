package invoice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrInvoiceNotFound is returned when a lookup finds no matching invoice
// within the given organisation, as opposed to a genuine database failure.
var ErrInvoiceNotFound = errors.New("invoice not found")

// dbExecutor is the minimal query surface Create, CreateLines, GetByID and
// GetLinesByInvoiceID need. Both *pgxpool.Pool and pgx.Tx implement it,
// which is what lets those methods run unmodified whether they're
// operating directly on the pool or inside a transaction the service is
// managing — the repository doesn't know or care which.
type dbExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresInvoiceRepository is the PostgreSQL-backed implementation of
// InvoiceRepository. It holds a connection pool rather than creating one
// itself, so the caller decides how the pool is configured and when it is
// closed.
type PostgresInvoiceRepository struct {
	db dbExecutor
}

// NewPostgresInvoiceRepository wires an existing pool into a repository.
func NewPostgresInvoiceRepository(db *pgxpool.Pool) *PostgresInvoiceRepository {
	return &PostgresInvoiceRepository{
		db: db,
	}
}

// WithTx returns a repository that runs Create and CreateLines against tx
// instead of the pool, so an invoice and its lines can be written within
// one caller-managed transaction. It does not begin, commit or roll back
// anything itself — tx is already open, and finishing it remains the
// caller's responsibility.
func (r *PostgresInvoiceRepository) WithTx(tx pgx.Tx) InvoiceRepository {
	return &PostgresInvoiceRepository{
		db: tx,
	}
}

// Create inserts a new invoice row. created_at and updated_at are left to
// PostgreSQL's DEFAULT NOW(), and deleted_at stays NULL until the invoice
// is soft-deleted — the two DB-generated values are scanned straight
// back onto invoice via RETURNING, so InvoiceService.Create's caller (the
// HTTP handler, building InvoiceResponse directly from this same
// *Invoice) gets real timestamps without a second SELECT.
//
// This does not also create the invoice's lines — see CreateLines. The two
// are separate statements, but InvoiceService.Create runs both of them
// through the same transaction (via WithTx), so a failure in either one
// rolls back the other.
func (r *PostgresInvoiceRepository) Create(
	ctx context.Context,
	invoice *Invoice,
) error {
	const query = `
		INSERT INTO invoices (
			id,
			organisation_id,
			customer_id,
			invoice_number,
			issue_date,
			due_date,
			subtotal,
			vat_total,
			total,
			status,
			notes,
			vat_registered
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		invoice.ID,
		invoice.OrganisationID,
		invoice.CustomerID,
		invoice.InvoiceNumber,
		invoice.IssueDate,
		invoice.DueDate,
		invoice.Subtotal,
		invoice.VATTotal,
		invoice.Total,
		invoice.Status,
		invoice.Notes,
		invoice.VATRegistered,
	).Scan(&invoice.CreatedAt, &invoice.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create invoice: %w", err)
	}

	return nil
}

// CreateLines inserts one row per line. Each line is still its own Exec
// call, but when this repository was obtained via WithTx, every one of
// those Execs runs against the same open transaction — so if a later line
// fails, InvoiceService rolls back the whole transaction and any
// already-inserted lines from this same call are undone along with it.
func (r *PostgresInvoiceRepository) CreateLines(
	ctx context.Context,
	lines []*Line,
) error {
	const query = `
		INSERT INTO invoice_lines (
			id,
			invoice_id,
			product_id,
			description,
			quantity,
			unit_price,
			vat_rate,
			vat_amount,
			total
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	for _, line := range lines {
		_, err := r.db.Exec(
			ctx,
			query,
			line.ID,
			line.InvoiceID,
			line.ProductID,
			line.Description,
			line.Quantity,
			line.UnitPrice,
			line.VATRate,
			line.VATAmount,
			line.Total,
		)
		if err != nil {
			return fmt.Errorf("create invoice line: %w", err)
		}
	}

	return nil
}

// GetByID fetches a single invoice by its primary key, scoped to the
// supplied organisation so one organisation can never read another's
// invoice. Soft-deleted invoices are excluded.
func (r *PostgresInvoiceRepository) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (*Invoice, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			customer_id,
			invoice_number,
			issue_date,
			due_date,
			subtotal,
			vat_total,
			total,
			status,
			sent_at,
			notes,
			vat_registered,
			seller_name, seller_email, seller_phone, seller_website,
			seller_address, seller_city, seller_state, seller_postal_code,
			seller_country, seller_tax_id,
			customer_name, customer_company_name, customer_email, customer_phone,
			customer_tax_id, customer_address, customer_city, customer_state,
			customer_postal_code, customer_country,
			currency,
			created_at,
			updated_at,
			deleted_at
		FROM invoices
		WHERE organisation_id = $1
			AND id = $2
			AND deleted_at IS NULL
	`

	var inv Invoice

	err := r.db.QueryRow(
		ctx,
		query,
		organisationID,
		invoiceID,
	).Scan(
		&inv.ID,
		&inv.OrganisationID,
		&inv.CustomerID,
		&inv.InvoiceNumber,
		&inv.IssueDate,
		&inv.DueDate,
		&inv.Subtotal,
		&inv.VATTotal,
		&inv.Total,
		&inv.Status,
		&inv.SentAt,
		&inv.Notes,
		&inv.VATRegistered,
		&inv.SellerName, &inv.SellerEmail, &inv.SellerPhone, &inv.SellerWebsite,
		&inv.SellerAddress, &inv.SellerCity, &inv.SellerState, &inv.SellerPostalCode,
		&inv.SellerCountry, &inv.SellerTaxID,
		&inv.CustomerName, &inv.CustomerCompanyName, &inv.CustomerEmail, &inv.CustomerPhone,
		&inv.CustomerTaxID, &inv.CustomerAddress, &inv.CustomerCity, &inv.CustomerState,
		&inv.CustomerPostalCode, &inv.CustomerCountry,
		&inv.Currency,
		&inv.CreatedAt,
		&inv.UpdatedAt,
		&inv.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvoiceNotFound
		}

		return nil, fmt.Errorf("get invoice by id: %w", err)
	}

	return &inv, nil
}

// GetForUpdate fetches a single invoice by its primary key, scoped to the
// supplied organisation exactly like GetByID, but additionally locks the
// row with FOR UPDATE. The lock is held by PostgreSQL until whichever
// transaction this repository was obtained via WithTx for is committed or
// rolled back — calling this outside of a transaction (i.e. against the
// bare pool) still works, but the lock is released the instant this
// statement finishes, so it provides no protection in that case. Callers
// doing payment creation must always go through WithTx first. Soft-deleted
// invoices are excluded.
func (r *PostgresInvoiceRepository) GetForUpdate(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (*Invoice, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			customer_id,
			invoice_number,
			issue_date,
			due_date,
			subtotal,
			vat_total,
			total,
			status,
			sent_at,
			notes,
			vat_registered,
			seller_name, seller_email, seller_phone, seller_website,
			seller_address, seller_city, seller_state, seller_postal_code,
			seller_country, seller_tax_id,
			customer_name, customer_company_name, customer_email, customer_phone,
			customer_tax_id, customer_address, customer_city, customer_state,
			customer_postal_code, customer_country,
			currency,
			created_at,
			updated_at,
			deleted_at
		FROM invoices
		WHERE organisation_id = $1
			AND id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`

	var inv Invoice

	err := r.db.QueryRow(
		ctx,
		query,
		organisationID,
		invoiceID,
	).Scan(
		&inv.ID,
		&inv.OrganisationID,
		&inv.CustomerID,
		&inv.InvoiceNumber,
		&inv.IssueDate,
		&inv.DueDate,
		&inv.Subtotal,
		&inv.VATTotal,
		&inv.Total,
		&inv.Status,
		&inv.SentAt,
		&inv.Notes,
		&inv.VATRegistered,
		&inv.SellerName, &inv.SellerEmail, &inv.SellerPhone, &inv.SellerWebsite,
		&inv.SellerAddress, &inv.SellerCity, &inv.SellerState, &inv.SellerPostalCode,
		&inv.SellerCountry, &inv.SellerTaxID,
		&inv.CustomerName, &inv.CustomerCompanyName, &inv.CustomerEmail, &inv.CustomerPhone,
		&inv.CustomerTaxID, &inv.CustomerAddress, &inv.CustomerCity, &inv.CustomerState,
		&inv.CustomerPostalCode, &inv.CustomerCountry,
		&inv.Currency,
		&inv.CreatedAt,
		&inv.UpdatedAt,
		&inv.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvoiceNotFound
		}

		return nil, fmt.Errorf("get invoice for update: %w", err)
	}

	return &inv, nil
}

// UpdateStatus persists a new status for the invoice. organisationID is
// part of the UPDATE's own WHERE clause (Milestone 4 Part 6): this cannot
// mutate another organisation's invoice merely because a caller forgot to
// check ownership first — it independently re-checks, even though every
// current caller has already locked the row via GetForUpdate in the same
// transaction. A row that exists but belongs to a different organisation
// is indistinguishable from one that doesn't exist at all: both leave
// RowsAffected at 0 and return ErrInvoiceNotFound, preserving the
// existing not-found semantics rather than introducing a new,
// externally-distinguishable error.
func (r *PostgresInvoiceRepository) UpdateStatus(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
	status string,
) error {
	const query = `
		UPDATE invoices
		SET status = $1,
			updated_at = NOW()
		WHERE id = $2
			AND organisation_id = $3
			AND deleted_at IS NULL
	`

	tag, err := r.db.Exec(ctx, query, status, invoiceID, organisationID)
	if err != nil {
		return fmt.Errorf("update invoice status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrInvoiceNotFound
	}

	return nil
}

// MarkSentWithSnapshot persists the Draft -> Sent transition together
// with its immutable party snapshot: status, sent_at, and every
// SellerXxx/CustomerXxx/Currency column are set together in one UPDATE
// statement (Milestone 5, extended Milestone 7 Part 2), so none of them
// can ever be observably out of sync with each other, even under a
// failure — either every one of them changes, or (if this statement
// itself never runs, e.g. the enclosing transaction rolled back) none of
// them does. inv is the already-mutated Invoice a prior, successful
// Invoice.MarkSent call produced — this writes its Status/SentAt/snapshot
// fields exactly as they are on inv, not values re-derived here. It
// carries the same organisation_id predicate as UpdateStatus, for the
// same defense-in-depth reason.
//
// This does not also guard against a non-Draft current status in SQL:
// InvoiceService.Send already holds this row's FOR UPDATE lock and has
// already checked Invoice.MarkSent (the in-memory guard) inside the same
// transaction before ever calling this method, so no concurrent
// modification of status can occur between that check and this write —
// adding a redundant "AND status = 'draft'" clause here would only add a
// second, harder-to-diagnose way to reach RowsAffected() == 0 without
// closing any gap the lock doesn't already close.
func (r *PostgresInvoiceRepository) MarkSentWithSnapshot(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
	inv *Invoice,
) error {
	const query = `
		UPDATE invoices
		SET status = $1,
			sent_at = $2,
			seller_name = $3, seller_email = $4, seller_phone = $5, seller_website = $6,
			seller_address = $7, seller_city = $8, seller_state = $9, seller_postal_code = $10,
			seller_country = $11, seller_tax_id = $12,
			customer_name = $13, customer_company_name = $14, customer_email = $15, customer_phone = $16,
			customer_tax_id = $17, customer_address = $18, customer_city = $19, customer_state = $20,
			customer_postal_code = $21, customer_country = $22,
			currency = $23,
			updated_at = NOW()
		WHERE id = $24
			AND organisation_id = $25
			AND deleted_at IS NULL
	`

	tag, err := r.db.Exec(
		ctx,
		query,
		inv.Status,
		inv.SentAt,
		inv.SellerName, inv.SellerEmail, inv.SellerPhone, inv.SellerWebsite,
		inv.SellerAddress, inv.SellerCity, inv.SellerState, inv.SellerPostalCode,
		inv.SellerCountry, inv.SellerTaxID,
		inv.CustomerName, inv.CustomerCompanyName, inv.CustomerEmail, inv.CustomerPhone,
		inv.CustomerTaxID, inv.CustomerAddress, inv.CustomerCity, inv.CustomerState,
		inv.CustomerPostalCode, inv.CustomerCountry,
		inv.Currency,
		invoiceID,
		organisationID,
	)
	if err != nil {
		return fmt.Errorf("mark invoice sent with snapshot: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrInvoiceNotFound
	}

	return nil
}

// GetLinesByInvoiceID fetches every line belonging to an invoice, ordered
// by creation. organisationID is required (Milestone 4 Part 6) and
// enforced via a join back to invoices — invoice_lines has no
// organisation_id column of its own, so this is how the query itself
// scopes the read, rather than depending solely on the caller having
// already confirmed ownership via GetByID/GetForUpdate first. A line
// belonging to another organisation's invoice is indistinguishable from
// no lines at all: this returns an empty slice, exactly as it would for
// an invoice with genuinely no lines, so no cross-tenant existence leaks
// through a different result shape.
func (r *PostgresInvoiceRepository) GetLinesByInvoiceID(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) ([]*Line, error) {
	const query = `
		SELECT
			il.id,
			il.invoice_id,
			il.product_id,
			il.description,
			il.quantity,
			il.unit_price,
			il.vat_rate,
			il.vat_amount,
			il.total,
			il.created_at,
			il.updated_at
		FROM invoice_lines il
		JOIN invoices i ON i.id = il.invoice_id
		WHERE il.invoice_id = $1
			AND i.organisation_id = $2
		ORDER BY il.created_at
	`

	rows, err := r.db.Query(ctx, query, invoiceID, organisationID)
	if err != nil {
		return nil, fmt.Errorf("get invoice lines: %w", err)
	}
	defer rows.Close()

	var lines []*Line

	for rows.Next() {
		var l Line

		if err := rows.Scan(
			&l.ID,
			&l.InvoiceID,
			&l.ProductID,
			&l.Description,
			&l.Quantity,
			&l.UnitPrice,
			&l.VATRate,
			&l.VATAmount,
			&l.Total,
			&l.CreatedAt,
			&l.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invoice line: %w", err)
		}

		lines = append(lines, &l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read invoice lines: %w", err)
	}

	return lines, nil
}

// invoiceSortColumns maps List's public, httpx.ParseSortOrder-validated
// sort field names onto actual SQL columns — the one place a client-
// controlled value is allowed to influence an ORDER BY, and only via
// this fixed, explicit lookup, never by interpolating the field name
// itself.
var invoiceSortColumns = map[string]string{
	"invoiceNumber": "invoice_number",
	"issueDate":     "issue_date",
	"dueDate":       "due_date",
	"total":         "total",
	"createdAt":     "created_at",
}

// List returns the page of invoices matching filter, tenant-scoped to
// organisationID, together with the total count of invoices matching the
// same filters. The WHERE clause is built once and reused for both the
// item query and the COUNT(*) query, so the two can never drift apart.
//
// Effective-status filtering (Milestone 8 Part 3 section 9) is the one
// subtle piece here: "sent" and "overdue" are never persisted as such
// for the "overdue" half — a persisted Sent invoice is filtered by
// comparing due_date against today, the exact same boundary
// Invoice.EffectiveStatus uses (today's calendar date strictly after
// due_date means overdue; on or after today means still "sent"). status
// = 'draft' and status = 'paid' are the two genuinely persisted, 1:1
// filters — no date comparison needed for either.
//
// Search is bound as a single "%term%" parameter matched via ILIKE
// against invoice_number; '%'/'_' in the term itself remain live
// wildcards, matching every other List in this project.
func (r *PostgresInvoiceRepository) List(
	ctx context.Context,
	organisationID uuid.UUID,
	filter InvoiceListFilter,
	today time.Time,
) ([]*Invoice, int64, error) {
	conditions := []string{"organisation_id = $1", "deleted_at IS NULL"}
	args := []any{organisationID}

	switch filter.Status {
	case InvoiceStatusDraft:
		conditions = append(conditions, "status = 'draft'")
	case InvoiceStatusPaid:
		conditions = append(conditions, "status = 'paid'")
	case InvoiceStatusSent:
		args = append(args, today)
		conditions = append(conditions, fmt.Sprintf("status = 'sent' AND due_date >= $%d", len(args)))
	case InvoiceStatusOverdue:
		args = append(args, today)
		conditions = append(conditions, fmt.Sprintf("status = 'sent' AND due_date < $%d", len(args)))
	}

	if filter.CustomerID != nil {
		args = append(args, *filter.CustomerID)
		conditions = append(conditions, fmt.Sprintf("customer_id = $%d", len(args)))
	}

	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		conditions = append(conditions, fmt.Sprintf("invoice_number ILIKE $%d", len(args)))
	}

	if filter.IssueDateFrom != nil {
		args = append(args, *filter.IssueDateFrom)
		conditions = append(conditions, fmt.Sprintf("issue_date >= $%d", len(args)))
	}
	if filter.IssueDateTo != nil {
		args = append(args, *filter.IssueDateTo)
		conditions = append(conditions, fmt.Sprintf("issue_date <= $%d", len(args)))
	}
	if filter.DueDateFrom != nil {
		args = append(args, *filter.DueDateFrom)
		conditions = append(conditions, fmt.Sprintf("due_date >= $%d", len(args)))
	}
	if filter.DueDateTo != nil {
		args = append(args, *filter.DueDateTo)
		conditions = append(conditions, fmt.Sprintf("due_date <= $%d", len(args)))
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	sortColumn, ok := invoiceSortColumns[filter.Sort]
	if !ok {
		sortColumn = "issue_date"
	}

	direction := "DESC"
	if strings.EqualFold(filter.Order, "asc") {
		direction = "ASC"
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM invoices " + whereClause
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count invoices: %w", err)
	}

	// id ASC is a stable secondary key for every sort column — even
	// invoice_number, which is unique per organisation but not globally,
	// applied uniformly for simplicity. This query intentionally selects
	// only the invoice-level columns a list row needs to build an
	// InvoiceResponse with an empty Lines slice (see InvoiceService.List
	// for why list rows never fetch per-invoice line detail) — the full
	// seller/customer snapshot columns aren't needed either, since list
	// responses don't expose them, matching GetByID's own column set
	// being a superset used only where actually read.
	itemQuery := fmt.Sprintf(
		`SELECT
			id, organisation_id, customer_id, invoice_number, issue_date,
			due_date, subtotal, vat_total, total, status, sent_at, notes,
			vat_registered, currency, created_at, updated_at
		FROM invoices
		%s
		ORDER BY %s %s, id ASC
		LIMIT $%d OFFSET $%d`,
		whereClause, sortColumn, direction, len(args)+1, len(args)+2,
	)

	rows, err := r.db.Query(ctx, itemQuery, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	var invoices []*Invoice

	for rows.Next() {
		var inv Invoice

		if err := rows.Scan(
			&inv.ID,
			&inv.OrganisationID,
			&inv.CustomerID,
			&inv.InvoiceNumber,
			&inv.IssueDate,
			&inv.DueDate,
			&inv.Subtotal,
			&inv.VATTotal,
			&inv.Total,
			&inv.Status,
			&inv.SentAt,
			&inv.Notes,
			&inv.VATRegistered,
			&inv.Currency,
			&inv.CreatedAt,
			&inv.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan invoice: %w", err)
		}

		invoices = append(invoices, &inv)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read invoices: %w", err)
	}

	return invoices, total, nil
}
