package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrUserNotFound is returned when a lookup finds no matching user, as
// opposed to a genuine database failure.
var ErrUserNotFound = errors.New("user not found")

// ErrUserEmailAlreadyExists is returned when Create violates the
// users_email_unique constraint, as opposed to a genuine database
// failure. Email uniqueness is global (Milestone 4 Part 2) — the same
// email can no longer be reused by a second organisation.
var ErrUserEmailAlreadyExists = errors.New("user email already exists")

// postgresUniqueViolation is the PostgreSQL SQLSTATE code for a unique
// constraint violation.
const postgresUniqueViolation = "23505"

// PostgresUserRepository is the PostgreSQL-backed implementation of
// UserRepository. It holds a dbExecutor (see settings_repository_postgres.go)
// rather than a concrete pool, so the same code runs unmodified whether
// it's operating directly on the pool or inside a transaction WithTx
// provides.
type PostgresUserRepository struct {
	db dbExecutor
}

// NewPostgresUserRepository wires an existing pool into a repository.
func NewPostgresUserRepository(db *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{
		db: db,
	}
}

// WithTx returns a repository that runs its operations against tx instead
// of the pool, so a User.LastLogin update can be committed within the
// same caller-managed transaction as a new session's creation. It does
// not begin, commit or roll back anything itself.
func (r *PostgresUserRepository) WithTx(tx pgx.Tx) UserRepository {
	return &PostgresUserRepository{
		db: tx,
	}
}

// Create inserts a new user row. created_at and updated_at are left to
// PostgreSQL's DEFAULT NOW(), and deleted_at/last_login stay NULL.
//
// Email uniqueness is enforced by the users_email_unique database
// constraint, not by a pre-emptive lookup here — that would introduce a
// TOCTOU race between the check and the insert. A violation of that
// constraint is translated into ErrUserEmailAlreadyExists; the raw
// PostgreSQL error never reaches callers for this case.
func (r *PostgresUserRepository) Create(
	ctx context.Context,
	user *User,
) error {
	const query = `
		INSERT INTO users (
			id,
			organisation_id,
			name,
			email,
			password_hash,
			role,
			is_active
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`

	_, err := r.db.Exec(
		ctx,
		query,
		user.ID,
		user.OrganisationID,
		user.Name,
		user.Email,
		user.PasswordHash,
		user.Role,
		user.IsActive,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation {
			return ErrUserEmailAlreadyExists
		}

		return fmt.Errorf("create user: %w", err)
	}

	return nil
}

// GetByID fetches a single user by their primary key, scoped to the
// supplied organisation so one organisation can never read another's
// user. Soft-deleted users are excluded.
func (r *PostgresUserRepository) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	userID uuid.UUID,
) (*User, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			name,
			email,
			password_hash,
			role,
			is_active,
			last_login,
			created_at,
			updated_at,
			deleted_at
		FROM users
		WHERE organisation_id = $1
			AND id = $2
			AND deleted_at IS NULL
	`

	return r.scanOne(ctx, query, organisationID, userID)
}

// GetByEmail fetches a single user by email, globally — not scoped to any
// organisation. users_email_unique (Milestone 4 Part 2) guarantees at
// most one non-deleted user can ever match. Soft-deleted users are
// excluded.
func (r *PostgresUserRepository) GetByEmail(
	ctx context.Context,
	email string,
) (*User, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			name,
			email,
			password_hash,
			role,
			is_active,
			last_login,
			created_at,
			updated_at,
			deleted_at
		FROM users
		WHERE email = $1
			AND deleted_at IS NULL
	`

	return r.scanOne(ctx, query, email)
}

// GetByIDForAuthentication fetches a user by primary key only, with no
// organisation scoping — see UserRepository's doc comment for why this
// exists and who may call it. Soft-deleted users are excluded, the same
// as every other lookup on this repository.
func (r *PostgresUserRepository) GetByIDForAuthentication(
	ctx context.Context,
	userID uuid.UUID,
) (*User, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			name,
			email,
			password_hash,
			role,
			is_active,
			last_login,
			created_at,
			updated_at,
			deleted_at
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
	`

	return r.scanOne(ctx, query, userID)
}

// UpdateLastLogin persists the moment a user last successfully
// authenticated. Called through AuthService.Login's transaction with at
// set to the same instant used to compute the new session's expiry, so
// LastLogin and the session's CreatedAt always agree.
func (r *PostgresUserRepository) UpdateLastLogin(
	ctx context.Context,
	userID uuid.UUID,
	at time.Time,
) error {
	const query = `
		UPDATE users
		SET last_login = $1,
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
	`

	tag, err := r.db.Exec(ctx, query, at, userID)
	if err != nil {
		return fmt.Errorf("update user last login: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// scanOne runs a single-row user query (used by both GetByID and
// GetByEmail, which differ only in their SQL) and translates
// pgx.ErrNoRows into ErrUserNotFound.
func (r *PostgresUserRepository) scanOne(ctx context.Context, query string, args ...any) (*User, error) {
	var u User

	err := r.db.QueryRow(ctx, query, args...).Scan(
		&u.ID,
		&u.OrganisationID,
		&u.Name,
		&u.Email,
		&u.PasswordHash,
		&u.Role,
		&u.IsActive,
		&u.LastLogin,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}

		return nil, fmt.Errorf("get user: %w", err)
	}

	return &u, nil
}
