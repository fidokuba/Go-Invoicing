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

	// GetTotalPaidByInvoiceIDs (Milestone 8 Part 3) is GetTotalPaidByInvoiceID's
	// batch counterpart, purpose-built for GET /invoices: one grouped
	// aggregate query for an entire page of invoices, instead of one
	// query per invoice (the N+1 pattern the list endpoint must avoid).
	// The returned map has an entry only for invoice IDs with at least
	// one payment — an ID absent from the map has paid nothing, which
	// Go's zero value for int64 already represents correctly when the
	// caller indexes the map for an ID that isn't there.
	GetTotalPaidByInvoiceIDs(ctx context.Context, organisationID uuid.UUID, invoiceIDs []uuid.UUID) (map[uuid.UUID]int64, error)
}
