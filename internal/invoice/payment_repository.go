package invoice

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PaymentRepository describes how payments are read from and written to
// storage. Payments have no organisation_id column of their own — the
// schema deliberately isn't changing to add one (Milestone 4 Part 6) — so
// ownership is established through the parent invoice: every method here
// takes organisationID explicitly and scopes its SQL through invoices,
// rather than relying solely on InvoiceService having already confirmed
// ownership (via GetByID/GetForUpdate) before ever calling in here. That
// service-level check still happens and is still the primary guard; this
// is deliberate, independent, redundant scoping at the repository layer —
// defense in depth, not a replacement.
//
// WithTx returns a repository whose Create runs against the supplied
// transaction instead of the default connection pool. The repository
// itself never calls Begin, Commit or Rollback — that ownership belongs to
// whichever service orchestrates a payment's creation, matching the
// pattern already used by InvoiceRepository.
type PaymentRepository interface {
	WithTx(tx pgx.Tx) PaymentRepository

	// Create inserts a payment for invoiceID only if that invoice
	// actually belongs to organisationID — see the Postgres
	// implementation for how an INSERT ... SELECT enforces this in a
	// single statement. If no such tenant-owned invoice exists, this
	// returns ErrInvoiceNotFound rather than silently inserting nothing.
	Create(ctx context.Context, organisationID uuid.UUID, payment *Payment) error
	GetByInvoiceID(ctx context.Context, organisationID uuid.UUID, invoiceID uuid.UUID) ([]*Payment, error)
	GetTotalPaidByInvoiceID(ctx context.Context, organisationID uuid.UUID, invoiceID uuid.UUID) (int64, error)
}
