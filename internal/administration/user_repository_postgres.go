package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// PostgreSQL's DEFAULT NOW(), and deleted_at/last_login stay NULL — the
// two DB-generated values are scanned straight back onto user via
// RETURNING, so a caller building an API response directly from this
// same *User (RegistrationService.Register, UserService.Create) gets
// real timestamps without a second SELECT.
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
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		user.ID,
		user.OrganisationID,
		user.Name,
		user.Email,
		user.PasswordHash,
		user.Role,
		user.IsActive,
	).Scan(&user.CreatedAt, &user.UpdatedAt)
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

// userSortColumns maps List's public, httpx.ParseSortOrder-validated
// sort field names onto actual SQL columns — the one place a client-
// controlled value is allowed to influence an ORDER BY, and only via
// this fixed, explicit lookup, never by interpolating the field name
// itself.
var userSortColumns = map[string]string{
	"email":     "email",
	"role":      "role",
	"createdAt": "created_at",
}

// List returns the page of users matching filter, tenant-scoped to
// organisationID, together with the total count of users matching the
// same filters. The WHERE clause is built once and reused for both the
// item query and the COUNT(*) query, so the two can never drift apart.
func (r *PostgresUserRepository) List(
	ctx context.Context,
	organisationID uuid.UUID,
	filter UserListFilter,
) ([]*User, int64, error) {
	conditions := []string{"organisation_id = $1", "deleted_at IS NULL"}
	args := []any{organisationID}

	if filter.Role != "" {
		args = append(args, filter.Role)
		conditions = append(conditions, fmt.Sprintf("role = $%d", len(args)))
	}

	if filter.Active != nil {
		args = append(args, *filter.Active)
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", len(args)))
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	sortColumn, ok := userSortColumns[filter.Sort]
	if !ok {
		sortColumn = "created_at"
	}

	direction := "ASC"
	if strings.EqualFold(filter.Order, "desc") {
		direction = "DESC"
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM users " + whereClause
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	// id ASC is a stable secondary key — email is globally unique but not
	// per-organisation-ordering-relevant here, role and created_at are
	// both non-unique.
	itemQuery := fmt.Sprintf(
		`SELECT
			id, organisation_id, name, email, password_hash, role,
			is_active, last_login, created_at, updated_at
		FROM users
		%s
		ORDER BY %s %s, id ASC
		LIMIT $%d OFFSET $%d`,
		whereClause, sortColumn, direction, len(args)+1, len(args)+2,
	)

	rows, err := r.db.Query(ctx, itemQuery, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User

	for rows.Next() {
		var u User

		if err := rows.Scan(
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
		); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}

		users = append(users, &u)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read users: %w", err)
	}

	return users, total, nil
}
