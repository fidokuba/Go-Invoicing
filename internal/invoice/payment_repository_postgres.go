package invoice

import (
	"context"
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

// Create inserts a new payment row. created_at and updated_at are left to
// PostgreSQL's DEFAULT NOW(), matching every other Create in this
// project (Invoice, Line, Organisation, Customer, Product, Settings).
func (r *PostgresPaymentRepository) Create(
	ctx context.Context,
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
		VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`

	_, err := r.db.Exec(
		ctx,
		query,
		payment.ID,
		payment.InvoiceID,
		payment.Amount,
		payment.PaymentMethod,
		payment.PaymentDate,
		payment.Reference,
		payment.Notes,
	)
	if err != nil {
		return fmt.Errorf("create payment: %w", err)
	}

	return nil
}

// GetByInvoiceID fetches every payment belonging to an invoice, ordered by
// payment_date and then created_at so results are deterministic even when
// multiple payments share the same payment_date. It does not itself check
// organisation scope — see the PaymentRepository doc comment.
func (r *PostgresPaymentRepository) GetByInvoiceID(
	ctx context.Context,
	invoiceID uuid.UUID,
) ([]*Payment, error) {
	const query = `
		SELECT
			id,
			invoice_id,
			amount,
			payment_method,
			payment_date,
			reference,
			notes,
			created_at,
			updated_at
		FROM payments
		WHERE invoice_id = $1
		ORDER BY payment_date, created_at
	`

	rows, err := r.db.Query(ctx, query, invoiceID)
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
// NULL result when the invoice has no payments at all, so this returns 0
// in that case rather than a NULL-scan error.
func (r *PostgresPaymentRepository) GetTotalPaidByInvoiceID(
	ctx context.Context,
	invoiceID uuid.UUID,
) (int64, error) {
	const query = `
		SELECT COALESCE(SUM(amount), 0)
		FROM payments
		WHERE invoice_id = $1
	`

	var total int64

	if err := r.db.QueryRow(ctx, query, invoiceID).Scan(&total); err != nil {
		return 0, fmt.Errorf("get total paid: %w", err)
	}

	return total, nil
}
