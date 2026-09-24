package admin

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OrganisationRepository describes how organisations are read from and written
// to storage. It deliberately says nothing about PostgreSQL, HTTP or JSON, so a
// service depending on this interface can be tested against a fake and the
// storage engine can change without the service noticing.
//
// Every method takes a context.Context as its first argument so that a caller
// can cancel a slow query when, for example, the HTTP request behind it is
// abandoned.
//
// WithTx returns a repository whose operations run against the supplied
// transaction instead of the default connection pool, so an organisation
// can be created atomically alongside its default settings and its first
// user — see RegistrationService.Register. The repository itself never
// calls Begin, Commit or Rollback — the caller owns the transaction's
// lifecycle, matching the pattern already used by SettingsRepository and
// UserRepository.
type OrganisationRepository interface {
	WithTx(tx pgx.Tx) OrganisationRepository

	Create(ctx context.Context, organisation *Organisation) error
	GetByID(ctx context.Context, id uuid.UUID) (*Organisation, error)

	// Update persists organisation's mutable party-detail fields (Name,
	// Email, Phone, Website, Address, City, State, PostalCode, Country,
	// TaxID) — Logo is left untouched (Milestone 7 Part 1 doesn't manage
	// it), and ID/CreatedAt/DeletedAt are never written by this method.
	//
	// organisationID is taken explicitly, separately from organisation.ID
	// (even though OrganisationService.Update always passes the same
	// value for both, by construction — there is no client-supplied
	// organisation ID anywhere in this flow), and used in the query's own
	// WHERE clause — the same "don't rely solely on the caller having
	// already checked" defense-in-depth reasoning applied to every other
	// tenant-scoped repository mutation in this project (e.g.
	// InvoiceRepository.MarkSent).
	//
	// expectedVersion (Milestone 13 Part 2): the write only happens if the
	// organisation's current version equals it, atomically in the same
	// statement; otherwise ErrOrganisationVersionConflict. On success
	// organisation.Version holds the new, incremented version.
	Update(ctx context.Context, organisationID uuid.UUID, organisation *Organisation, expectedVersion int64) error
}
