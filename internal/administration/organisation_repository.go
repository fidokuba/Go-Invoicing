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
}
