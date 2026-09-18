package invoice

import (
	"context"
	"errors"
	"fmt"
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
// is soft-deleted.
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
			notes
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`

	_, err := r.db.Exec(
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
	)
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

// MarkSent persists the Draft -> Sent transition: status and sent_at are
// set together in one UPDATE statement (Milestone 5), so the two columns
// can never be observably out of sync with each other, even under a
// failure — either both change, or (if this statement itself never runs,
// e.g. the enclosing transaction rolled back) neither does. It carries
// the same organisation_id predicate as UpdateStatus, for the same
// defense-in-depth reason.
//
// This does not also guard against a non-Draft current status in SQL:
// InvoiceService.Send already holds this row's FOR UPDATE lock and has
// already checked Invoice.MarkSent (the in-memory guard) inside the same
// transaction before ever calling this method, so no concurrent
// modification of status can occur between that check and this write —
// adding a redundant "AND status = 'draft'" clause here would only add a
// second, harder-to-diagnose way to reach RowsAffected() == 0 without
// closing any gap the lock doesn't already close.
func (r *PostgresInvoiceRepository) MarkSent(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
	sentAt time.Time,
) error {
	const query = `
		UPDATE invoices
		SET status = $1,
			sent_at = $2,
			updated_at = NOW()
		WHERE id = $3
			AND organisation_id = $4
			AND deleted_at IS NULL
	`

	tag, err := r.db.Exec(ctx, query, InvoiceStatusSent, sentAt, invoiceID, organisationID)
	if err != nil {
		return fmt.Errorf("mark invoice sent: %w", err)
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
