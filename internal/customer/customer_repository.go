package customer

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListFilter narrows GET /customers (Milestone 8 Part 3) to a specific,
// fixed set of query capabilities — never a generic/reflection-based
// filter: every field here corresponds to one explicit, documented query
// parameter, mapped onto explicit SQL in the Postgres implementation.
//
// Search is matched via ILIKE against name, company_name and email — see
// PostgresCustomerRepository.List for why '%'/'_' remain live wildcard
// characters in the search term. Status, if non-empty, must already be
// one of the CustomerStatus* constants — ListFilter itself performs no
// validation; CustomerService.List is where an invalid status is
// rejected before this ever reaches the repository.
//
// Sort is a public field name already validated against a repository-
// known allow-list (see httpx.ParseSortOrder) — the repository maps it
// onto an actual SQL column via its own explicit switch, never by
// interpolating it directly. Order is "asc" or "desc".
type ListFilter struct {
	Search string
	Status string
	Sort   string
	Order  string
	Limit  int
	Offset int
}

// CustomerRepository describes how customers are read from and written to
// storage. GetByID is organisation-scoped: a customer can only be fetched
// through the organisation it belongs to, so one organisation's data can
// never leak into another's lookup.
//
// WithTx returns a repository whose operations run against the supplied
// transaction instead of the default connection pool (Milestone 7 Part
// 2) — needed so InvoiceService.Send can read a customer's identity
// fields for its snapshot within the same transaction that locks and
// finalises the invoice, exactly like SettingsRepository/
// OrganisationRepository already do. The repository itself never calls
// Begin, Commit or Rollback — the caller owns the transaction's
// lifecycle.
type CustomerRepository interface {
	WithTx(tx pgx.Tx) CustomerRepository

	Create(ctx context.Context, customer *Customer) error
	GetByID(ctx context.Context, organisationID uuid.UUID, customerID uuid.UUID) (*Customer, error)

	// List returns the page of customers matching filter, tenant-scoped
	// to organisationID, together with the total count of customers
	// matching the same filters (ignoring Limit/Offset) — see the
	// Postgres implementation for how the two queries share one WHERE
	// clause so they can never drift apart.
	List(ctx context.Context, organisationID uuid.UUID, filter ListFilter) ([]*Customer, int64, error)
}
