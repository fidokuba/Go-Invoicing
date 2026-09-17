package invoice

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InvoiceRepository describes how invoices and their lines are read from
// and written to storage. GetByID is organisation-scoped: an invoice can
// only be fetched through the organisation it belongs to.
//
// WithTx returns a repository whose Create and CreateLines run against the
// supplied transaction instead of the default connection pool, so an
// invoice and every one of its lines can be written atomically. The
// repository itself never calls Begin, Commit or Rollback — InvoiceService
// owns the transaction's lifecycle, since "create invoice with its lines"
// is the atomic business operation, not a repository-level concern.
type InvoiceRepository interface {
	WithTx(tx pgx.Tx) InvoiceRepository

	Create(ctx context.Context, invoice *Invoice) error
	CreateLines(ctx context.Context, lines []*Line) error
	GetByID(ctx context.Context, organisationID uuid.UUID, invoiceID uuid.UUID) (*Invoice, error)
	GetLinesByInvoiceID(ctx context.Context, invoiceID uuid.UUID) ([]*Line, error)
}
