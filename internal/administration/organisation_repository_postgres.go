package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrOrganisationNotFound is returned when a lookup finds no matching
// organisation, as opposed to a genuine database failure. Callers above the
// repository (service, handler) can use errors.Is to distinguish a 404 from
// a 500.
var ErrOrganisationNotFound = errors.New("organisation not found")

// ErrOrganisationVersionConflict (Milestone 13 Part 2) is returned by
// Update when the organisation exists but its current version is not the
// caller's expected one — someone else has modified it since the caller
// read it, so applying this write would silently overwrite their change.
var ErrOrganisationVersionConflict = errors.New("organisation has been modified since it was read")

// PostgresOrganisationRepository is the PostgreSQL-backed implementation of
// OrganisationRepository. It holds a dbExecutor (see
// settings_repository_postgres.go) rather than a concrete pool, so the
// same code runs unmodified whether it's operating directly on the pool
// or inside a transaction WithTx provides.
type PostgresOrganisationRepository struct {
	db dbExecutor
}

// NewPostgresOrganisationRepository wires an existing pool into a repository.
func NewPostgresOrganisationRepository(db *pgxpool.Pool) *PostgresOrganisationRepository {
	return &PostgresOrganisationRepository{
		db: db,
	}
}

// WithTx returns a repository that runs its operations against tx instead
// of the pool, so an organisation can be created within the same
// caller-managed transaction as its default settings and first user. It
// does not begin, commit or roll back anything itself.
func (r *PostgresOrganisationRepository) WithTx(tx pgx.Tx) OrganisationRepository {
	return &PostgresOrganisationRepository{
		db: tx,
	}
}

// Create inserts a new organisation row. created_at and updated_at are
// left to PostgreSQL's DEFAULT NOW(), and deleted_at stays NULL until the
// organisation is soft-deleted — but the two DB-generated values are
// scanned straight back onto organisation via RETURNING, rather than
// left at their Go zero value, so the struct this method mutates
// accurately reflects the row now persisted. A caller that builds an API
// response directly from this same *Organisation (as
// RegistrationService.Register and OrganisationService.Create both do)
// gets real timestamps without a second SELECT.
func (r *PostgresOrganisationRepository) Create(
	ctx context.Context,
	organisation *Organisation,
) error {
	const query = `
		INSERT INTO organisations (
			id,
			name,
			email,
			phone,
			website,
			logo,
			address,
			city,
			state,
			postal_code,
			country,
			tax_id
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		RETURNING created_at, updated_at, version
	`

	err := r.db.QueryRow(
		ctx,
		query,
		organisation.ID,
		organisation.Name,
		organisation.Email,
		organisation.Phone,
		organisation.Website,
		organisation.Logo,
		organisation.Address,
		organisation.City,
		organisation.State,
		organisation.PostalCode,
		organisation.Country,
		organisation.TaxID,
	).Scan(&organisation.CreatedAt, &organisation.UpdatedAt, &organisation.Version)
	if err != nil {
		return fmt.Errorf("create organisation: %w", err)
	}

	return nil
}

// GetByID fetches a single organisation by its primary key. It returns an
// error if no such organisation exists.
func (r *PostgresOrganisationRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*Organisation, error) {
	const query = `
		SELECT
			id,
			name,
			email,
			phone,
			website,
			logo,
			address,
			city,
			state,
			postal_code,
			country,
			tax_id,
			created_at,
			updated_at,
			deleted_at,
			version
		FROM organisations
		WHERE id = $1
	`

	var organisation Organisation

	err := r.db.QueryRow(
		ctx,
		query,
		id,
	).Scan(
		&organisation.ID,
		&organisation.Name,
		&organisation.Email,
		&organisation.Phone,
		&organisation.Website,
		&organisation.Logo,
		&organisation.Address,
		&organisation.City,
		&organisation.State,
		&organisation.PostalCode,
		&organisation.Country,
		&organisation.TaxID,
		&organisation.CreatedAt,
		&organisation.UpdatedAt,
		&organisation.DeletedAt,
		&organisation.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrganisationNotFound
		}

		return nil, fmt.Errorf("get organisation by id: %w", err)
	}

	return &organisation, nil
}

// Update persists every mutable party-detail field in one statement,
// but only if the organisation's current version is still
// expectedVersion (Milestone 13 Part 2): the version predicate in the
// UPDATE itself is the authoritative optimistic-concurrency check, so two
// writers holding the same version can never both succeed — whichever
// commits second matches no row. A successful write increments version
// by one; the new version and updated_at are scanned back onto
// organisation. Logo is intentionally absent from the SET list, so it is
// never touched here — see OrganisationRepository.Update's own comment.
//
// Zero rows means either a stale expectedVersion
// (ErrOrganisationVersionConflict) or no such live organisation
// (ErrOrganisationNotFound); a follow-up existence check tells the two
// apart.
func (r *PostgresOrganisationRepository) Update(
	ctx context.Context,
	organisationID uuid.UUID,
	organisation *Organisation,
	expectedVersion int64,
) error {
	const query = `
		UPDATE organisations
		SET
			name        = $1,
			email       = $2,
			phone       = $3,
			website     = $4,
			address     = $5,
			city        = $6,
			state       = $7,
			postal_code = $8,
			country     = $9,
			tax_id      = $10,
			updated_at  = NOW(),
			version     = version + 1
		WHERE id = $11
			AND deleted_at IS NULL
			AND version = $12
		RETURNING version, updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		organisation.Name,
		organisation.Email,
		organisation.Phone,
		organisation.Website,
		organisation.Address,
		organisation.City,
		organisation.State,
		organisation.PostalCode,
		organisation.Country,
		organisation.TaxID,
		organisationID,
		expectedVersion,
	).Scan(&organisation.Version, &organisation.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return r.missingOrStale(ctx, organisationID)
		}

		return fmt.Errorf("update organisation: %w", err)
	}

	return nil
}

// missingOrStale classifies an Update that matched no row: the
// organisation still exists (so the version was stale) or it doesn't.
func (r *PostgresOrganisationRepository) missingOrStale(ctx context.Context, organisationID uuid.UUID) error {
	const query = `SELECT EXISTS (SELECT 1 FROM organisations WHERE id = $1 AND deleted_at IS NULL)`

	var exists bool
	if err := r.db.QueryRow(ctx, query, organisationID).Scan(&exists); err != nil {
		return fmt.Errorf("check organisation exists: %w", err)
	}

	if exists {
		return ErrOrganisationVersionConflict
	}

	return ErrOrganisationNotFound
}
