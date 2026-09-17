package admin

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SettingsRepository describes how organisation settings are read from and
// written to storage.
//
// GetForUpdate is the transactional variant used by invoice number
// allocation: its SQL takes a row lock (FOR UPDATE) that PostgreSQL holds
// until the enclosing transaction commits or rolls back, so two
// concurrent invoice creations for the same organisation can never both
// read the same "last allocated number" and increment from it.
// GetByOrganisationID is a plain, non-locking read for callers that don't
// need that guarantee.
//
// WithTx returns a repository whose operations run against the supplied
// transaction instead of the default connection pool. The repository
// itself never calls Begin, Commit or Rollback — the caller owns the
// transaction's lifecycle, matching the pattern already used by
// InvoiceRepository.
type SettingsRepository interface {
	WithTx(tx pgx.Tx) SettingsRepository

	Create(ctx context.Context, settings *Settings) error
	GetByOrganisationID(ctx context.Context, organisationID uuid.UUID) (*Settings, error)
	GetForUpdate(ctx context.Context, organisationID uuid.UUID) (*Settings, error)
	UpdateInvoiceNumber(ctx context.Context, organisationID uuid.UUID, invoiceNumber int) error
}
