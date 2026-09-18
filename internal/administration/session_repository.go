package admin

import (
	"context"

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
}
