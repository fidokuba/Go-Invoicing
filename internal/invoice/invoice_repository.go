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

	// GetLinesByInvoiceID takes organisationID explicitly (Milestone 4
	// Part 6) even though every current caller has already confirmed
	// ownership via GetByID/GetForUpdate first: invoice_lines has no
	// organisation_id column of its own, so this scopes through invoices
	// in the query itself rather than relying solely on the caller having
	// checked first. Defense in depth — see the doc comment on
	// PaymentRepository for the same reasoning applied to payments.
	GetLinesByInvoiceID(ctx context.Context, organisationID uuid.UUID, invoiceID uuid.UUID) ([]*Line, error)

	// GetForUpdate is GetByID's locking counterpart, used by payment
	// creation: it locks the invoice row (FOR UPDATE) until the enclosing
	// transaction commits or rolls back, so a concurrent payment against
	// the same invoice cannot read the same outstanding balance this call
	// is about to act on. It is also how payment creation establishes
	// organisation scoping — the same WHERE organisation_id = $1 clause
	// GetByID uses.
	GetForUpdate(ctx context.Context, organisationID uuid.UUID, invoiceID uuid.UUID) (*Invoice, error)

	// UpdateStatus persists a new status for the invoice. Called through
	// the same transaction that acquired the GetForUpdate lock, so it
	// rolls back along with everything else if a later step fails.
	//
	// organisationID is included in the UPDATE's own WHERE clause
	// (Milestone 4 Part 6), not just relied upon via the earlier
	// GetForUpdate call in the same transaction: a repository operation
	// on tenant-owned data shouldn't depend solely on a caller having
	// already checked ownership. This is deliberate redundancy with the
	// service-level check, not a replacement for it.
	UpdateStatus(ctx context.Context, organisationID uuid.UUID, invoiceID uuid.UUID, status string) error
}
