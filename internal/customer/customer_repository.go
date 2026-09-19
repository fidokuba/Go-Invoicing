package customer

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
}
