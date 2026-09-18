package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSessionNotFound is returned when a lookup finds no matching session,
// as opposed to a genuine database failure.
var ErrSessionNotFound = errors.New("session not found")

// PostgresSessionRepository is the PostgreSQL-backed implementation of
// SessionRepository. It holds a dbExecutor (see settings_repository_postgres.go)
// rather than a concrete pool, so the same code runs unmodified whether
// it's operating directly on the pool or inside a transaction WithTx
// provides.
type PostgresSessionRepository struct {
	db dbExecutor
}

// NewPostgresSessionRepository wires an existing pool into a repository.
func NewPostgresSessionRepository(db *pgxpool.Pool) *PostgresSessionRepository {
	return &PostgresSessionRepository{
		db: db,
	}
}

// WithTx returns a repository that runs Create against tx instead of the
// pool, so a session can be written within the same caller-managed
// transaction as the User.LastLogin update. It does not begin, commit or
// roll back anything itself.
func (r *PostgresSessionRepository) WithTx(tx pgx.Tx) SessionRepository {
	return &PostgresSessionRepository{
		db: tx,
	}
}

// Create inserts a new session row. created_at, expires_at and the
// session's ID are all supplied explicitly by the caller (AuthService),
// not left to a database default, since expires_at must be computed from
// the exact same instant as created_at — see AuthService.Login.
func (r *PostgresSessionRepository) Create(
	ctx context.Context,
	session *Session,
) error {
	const query = `
		INSERT INTO sessions (
			id,
			user_id,
			token_hash,
			created_at,
			expires_at
		)
		VALUES (
			$1, $2, $3, $4, $5
		)
	`

	_, err := r.db.Exec(
		ctx,
		query,
		session.ID,
		session.UserID,
		session.TokenHash,
		session.CreatedAt,
		session.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	return nil
}

// GetByTokenHash fetches a session by its token hash, whatever its
// current validity — an expired or revoked session is still returned
// here, not filtered out; see SessionRepository's doc comment for why.
func (r *PostgresSessionRepository) GetByTokenHash(
	ctx context.Context,
	tokenHash string,
) (*Session, error) {
	const query = `
		SELECT
			id,
			user_id,
			token_hash,
			created_at,
			expires_at,
			revoked_at
		FROM sessions
		WHERE token_hash = $1
	`

	var s Session

	err := r.db.QueryRow(ctx, query, tokenHash).Scan(
		&s.ID,
		&s.UserID,
		&s.TokenHash,
		&s.CreatedAt,
		&s.ExpiresAt,
		&s.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}

		return nil, fmt.Errorf("get session by token hash: %w", err)
	}

	return &s, nil
}
