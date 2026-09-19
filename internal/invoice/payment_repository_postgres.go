package invoice

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresPaymentRepository is the PostgreSQL-backed implementation of
// PaymentRepository. It holds a connection pool rather than creating one
// itself, so the caller decides how the pool is configured and when it is
// closed.
//
// It reuses the dbExecutor interface already defined in
// invoice_repository_postgres.go — the same minimal Exec/Query/QueryRow
// surface, satisfied by both *pgxpool.Pool and pgx.Tx — rather than
// declaring a second, identical one.
type PostgresPaymentRepository struct {
	db dbExecutor
}

// NewPostgresPaymentRepository wires an existing pool into a repository.
func NewPostgresPaymentRepository(db *pgxpool.Pool) *PostgresPaymentRepository {
	return &PostgresPaymentRepository{
		db: db,
	}
}

// WithTx returns a repository that runs Create against tx instead of the
// pool. It does not begin, commit or roll back anything itself — tx is
// already open, and finishing it remains the caller's responsibility.
func (r *PostgresPaymentRepository) WithTx(tx pgx.Tx) PaymentRepository {
	return &PostgresPaymentRepository{
		db: tx,
	}
}

// Create inserts a new payment row via INSERT ... SELECT: the row is only
// inserted at all if organisationID and invoiceID together match a real,
// tenant-owned invoice (Milestone 4 Part 6) — invoice_id is sourced from
// the matched invoices row itself (i.id), not blindly trusted from the
// caller's payment.InvoiceID, so there is no way for this statement to
// insert a payment against an invoice it didn't independently verify.
// created_at and updated_at are left to PostgreSQL's DEFAULT NOW(), but
// scanned straight back onto payment via RETURNING, so a caller building
// an API response directly from this same *Payment gets real timestamps
// without a second SELECT.
//
// If no tenant-owned invoice matches, zero rows are inserted — RETURNING
// then yields no row at all, which this reports as pgx.ErrNoRows via
// QueryRow.Scan, translated to ErrInvoiceNotFound exactly as the
// previous RowsAffected()==0 check did. This is the same not-found
// domain error the service-level ownership check
// (InvoiceRepository.GetForUpdate) already returns for this case, so the
// two layers of scoping stay externally indistinguishable from each
// other and from a genuinely missing invoice.
func (r *PostgresPaymentRepository) Create(
	ctx context.Context,
	organisationID uuid.UUID,
	payment *Payment,
) error {
	const query = `
		INSERT INTO payments (
			id,
			invoice_id,
			amount,
			payment_method,
			payment_date,
			reference,
			notes
		)
		SELECT
			$1, i.id, $3, $4, $5, $6, $7
		FROM invoices i
		WHERE i.id = $2
			AND i.organisation_id = $8
			AND i.deleted_at IS NULL
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		payment.ID,
		payment.InvoiceID,
		payment.Amount,
		payment.PaymentMethod,
		payment.PaymentDate,
		payment.Reference,
		payment.Notes,
		organisationID,
	).Scan(&payment.CreatedAt, &payment.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvoiceNotFound
		}

		return fmt.Errorf("create payment: %w", err)
	}

	return nil
}

// GetByInvoiceID fetches every payment belonging to an invoice, ordered by
// payment_date and then created_at so results are deterministic even when
// multiple payments share the same payment_date. organisationID is
// enforced via a join back to invoices (Milestone 4 Part 6) — payments
// has no organisation_id column of its own. An invoice belonging to
// another organisation yields an empty slice, exactly as a genuinely
// payment-less invoice would, so no cross-tenant existence leaks through
// a different result shape.
func (r *PostgresPaymentRepository) GetByInvoiceID(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) ([]*Payment, error) {
	const query = `
		SELECT
			p.id,
			p.invoice_id,
			p.amount,
			p.payment_method,
			p.payment_date,
			p.reference,
			p.notes,
			p.created_at,
			p.updated_at
		FROM payments p
		JOIN invoices i ON i.id = p.invoice_id
		WHERE p.invoice_id = $1
			AND i.organisation_id = $2
		ORDER BY p.payment_date, p.created_at
	`

	rows, err := r.db.Query(ctx, query, invoiceID, organisationID)
	if err != nil {
		return nil, fmt.Errorf("get payments: %w", err)
	}
	defer rows.Close()

	var payments []*Payment

	for rows.Next() {
		var p Payment

		if err := rows.Scan(
			&p.ID,
			&p.InvoiceID,
			&p.Amount,
			&p.PaymentMethod,
			&p.PaymentDate,
			&p.Reference,
			&p.Notes,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan payment: %w", err)
		}

		payments = append(payments, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read payments: %w", err)
	}

	return payments, nil
}

// GetTotalPaidByInvoiceID sums every payment for an invoice in PostgreSQL
// rather than loading each row into Go. COALESCE guards against SUM's
// NULL result when the invoice has no payments at all — or when
// organisationID doesn't match the invoice's real owner (Milestone 4
// Part 6, enforced via the same join GetByInvoiceID uses) — so both cases
// return 0 rather than a NULL-scan error or a cross-tenant amount, and
// are indistinguishable from each other.
func (r *PostgresPaymentRepository) GetTotalPaidByInvoiceID(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (int64, error) {
	const query = `
		SELECT COALESCE(SUM(p.amount), 0)
		FROM payments p
		JOIN invoices i ON i.id = p.invoice_id
		WHERE p.invoice_id = $1
			AND i.organisation_id = $2
	`

	var total int64

	if err := r.db.QueryRow(ctx, query, invoiceID, organisationID).Scan(&total); err != nil {
		return 0, fmt.Errorf("get total paid: %w", err)
	}

	return total, nil
}

// GetTotalPaidByInvoiceIDs sums payments per invoice in one grouped
// query, tenant-scoped the same way GetTotalPaidByInvoiceID is (joined
// through invoices, never trusting invoiceIDs alone). An empty
// invoiceIDs skips the query entirely and returns an empty map — an
// empty page of invoices needs no payment totals at all.
func (r *PostgresPaymentRepository) GetTotalPaidByInvoiceIDs(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceIDs []uuid.UUID,
) (map[uuid.UUID]int64, error) {
	totals := make(map[uuid.UUID]int64, len(invoiceIDs))

	if len(invoiceIDs) == 0 {
		return totals, nil
	}

	const query = `
		SELECT p.invoice_id, SUM(p.amount)
		FROM payments p
		JOIN invoices i ON i.id = p.invoice_id
		WHERE i.organisation_id = $1
			AND p.invoice_id = ANY($2)
		GROUP BY p.invoice_id
	`

	rows, err := r.db.Query(ctx, query, organisationID, invoiceIDs)
	if err != nil {
		return nil, fmt.Errorf("get total paid for invoice list: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			invoiceID uuid.UUID
			total     int64
		)

		if err := rows.Scan(&invoiceID, &total); err != nil {
			return nil, fmt.Errorf("scan invoice payment total: %w", err)
		}

		totals[invoiceID] = total
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read invoice payment totals: %w", err)
	}

	return totals, nil
}
