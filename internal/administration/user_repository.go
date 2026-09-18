package admin

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserRepository describes how users are read from and written to
// storage.
//
// GetByID remains organisation-scoped: it's how normal, tenant-controlled
// application operations read a user, and an organisation must never be
// able to read another organisation's user by guessing its ID.
//
// GetByEmail, by contrast, is global: since users_email_unique (Milestone
// 4 Part 2) makes email a globally unique identifier, authentication can
// look a user up before it knows what organisation they belong to — that
// organisation is discovered from the returned User itself.
//
// WithTx returns a repository whose operations run against the supplied
// transaction instead of the default connection pool, so a session
// creation and a User.LastLogin update can be committed together — see
// AuthService.Login. The repository itself never calls Begin, Commit or
// Rollback — the caller owns the transaction's lifecycle, matching the
// pattern already used by InvoiceRepository and SettingsRepository.
type UserRepository interface {
	WithTx(tx pgx.Tx) UserRepository

	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, organisationID uuid.UUID, userID uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)

	// GetByIDForAuthentication fetches a user by primary key only, with no
	// organisation scoping. It exists exclusively for authentication
	// infrastructure (AuthMiddleware): a validated Session carries a
	// UserID but deliberately no OrganisationID (Session has no such
	// field), so authentication cannot use the organisation-scoped
	// GetByID above. The explicit "ForAuthentication" name is deliberate:
	// any other call site reaching for this method — bypassing tenant
	// scoping — should stand out immediately in code review. Every normal,
	// tenant-controlled operation must keep using GetByID.
	GetByIDForAuthentication(ctx context.Context, userID uuid.UUID) (*User, error)

	// UpdateLastLogin persists the moment a user last successfully
	// authenticated. Called through AuthService.Login's transaction,
	// alongside the new session's creation.
	UpdateLastLogin(ctx context.Context, userID uuid.UUID, at time.Time) error
}
