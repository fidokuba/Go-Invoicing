package invoice

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PaymentRepository describes how payments are read from and written to
// storage. Payments are scoped through their invoice_id, not directly by
// organisation — organisation scoping is enforced one level up, by
// InvoiceRepository.GetByID, before a caller ever has an invoiceID to pass
// here. It is not this repository's responsibility.
//
// WithTx returns a repository whose Create runs against the supplied
// transaction instead of the default connection pool. The repository
// itself never calls Begin, Commit or Rollback — that ownership belongs to
// whichever service orchestrates a payment's creation, matching the
// pattern already used by InvoiceRepository.
type PaymentRepository interface {
	WithTx(tx pgx.Tx) PaymentRepository

	Create(ctx context.Context, payment *Payment) error
	GetByInvoiceID(ctx context.Context, invoiceID uuid.UUID) ([]*Payment, error)
	GetTotalPaidByInvoiceID(ctx context.Context, invoiceID uuid.UUID) (int64, error)
}
