package admin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionRepository describes how sessions are read from and written to
// storage.
//
// WithTx returns a repository whose operations run against the supplied
// transaction instead of the default connection pool, so a session can be
// created atomically alongside the User.LastLogin update AuthService.Login
// performs. The repository itself never calls Begin, Commit or Rollback —
// the caller owns the transaction's lifecycle, matching the pattern
// already used by InvoiceRepository and SettingsRepository.
//
// GetByTokenHash takes an already-hashed token — callers (Part 3's
// authentication middleware) are expected to hash an incoming bearer
// token with the same algorithm AuthService uses before calling this. It
// returns whatever session matches, including an expired or revoked one:
// deciding whether a session is still usable is Session.IsValid's job,
// not this lookup's.
type SessionRepository interface {
	WithTx(tx pgx.Tx) SessionRepository

	Create(ctx context.Context, session *Session) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*Session, error)

	// DeleteExpired removes up to limit sessions that are no longer usable
	// and returns how many rows were actually deleted, as a single bounded
	// SQL DELETE — never by fetching candidates into Go and deleting them
	// one at a time. A session is eligible when expires_at <= now (Session
	// IsValid treats a session as invalid from the instant now reaches
	// ExpiresAt via now.Before(ExpiresAt), so cleanup uses the matching
	// "<=" boundary rather than "<" — otherwise a session already invalid
	// for authentication would sit in the table for one more cleanup cycle
	// looking untouched) or when RevokedAt is set at all, regardless of
	// ExpiresAt.
	//
	// now is supplied explicitly by the caller (SessionCleanupWorker)
	// rather than read from the wall clock here, so both the query and its
	// tests are deterministic. This is a maintenance operation, not a
	// tenant-scoped one: sessions have no organisation_id (see Session's
	// own doc comment), so there is no tenant predicate to apply here.
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error)
}
