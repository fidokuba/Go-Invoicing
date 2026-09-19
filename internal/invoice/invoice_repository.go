package invoice

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InvoiceListFilter narrows GET /invoices (Milestone 8 Part 3) to a
// specific, fixed set of query capabilities — never a generic/
// reflection-based filter.
//
// Status, when non-empty, must already be one of the four public
// InvoiceStatus* values (including InvoiceStatusOverdue, which is never
// persisted) — validated by InvoiceService.List, not here; see
// PostgresInvoiceRepository.List for how it's translated into an
// explicit SQL predicate using the exact same due-date boundary
// Invoice.EffectiveStatus uses, never "WHERE status = 'overdue'".
//
// Search is matched via ILIKE against invoice_number.
//
// IssueDateFrom/To and DueDateFrom/To are inclusive bounds; a nil
// pointer means "no bound on this side" — InvoiceService.List rejects a
// From strictly after its matching To before this ever reaches the
// repository.
//
// Sort is a public field name already validated against a repository-
// known allow-list (see httpx.ParseSortOrder); the repository maps it
// onto an actual SQL column via its own explicit switch.
type InvoiceListFilter struct {
	Status        string
	CustomerID    *uuid.UUID
	Search        string
	IssueDateFrom *time.Time
	IssueDateTo   *time.Time
	DueDateFrom   *time.Time
	DueDateTo     *time.Time
	Sort          string
	Order         string
	Limit         int
	Offset        int
}

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

	// MarkSentWithSnapshot (Milestone 5, extended Milestone 7 Part 2)
	// atomically persists the Draft -> Sent transition together with its
	// immutable party snapshot: status, sent_at, and every SellerXxx/
	// CustomerXxx/Currency column are set together in a single UPDATE
	// statement, so there is never an externally observable moment where
	// status is "sent" but its snapshot is absent or partial — the whole
	// transaction simply rolls back instead of persisting any of it. inv
	// is the already-mutated in-memory Invoice returned by a successful
	// Invoice.MarkSent call (its Status, SentAt, and every snapshot field
	// are read from it) — this is not merely "write sentAt", the way
	// Milestone 5's original MarkSent was. Called through the same
	// GetForUpdate-locked transaction as UpdateStatus, and carries the
	// same organisation_id predicate for the same defense-in-depth reason.
	MarkSentWithSnapshot(ctx context.Context, organisationID uuid.UUID, invoiceID uuid.UUID, inv *Invoice) error

	// List returns the page of invoices matching filter, tenant-scoped
	// to organisationID, together with the total count of invoices
	// matching the same filters (ignoring Limit/Offset). today is the
	// UTC calendar date (see UTCDate) InvoiceService.List computed from
	// its own now — used only for the effective-overdue predicate when
	// filter.Status is "sent" or "overdue" — so a repeated call within
	// the same request always agrees with that request's own
	// EffectiveStatus calculations, and tests can pass a fixed value for
	// deterministic boundary testing.
	List(ctx context.Context, organisationID uuid.UUID, filter InvoiceListFilter, today time.Time) ([]*Invoice, int64, error)
}
