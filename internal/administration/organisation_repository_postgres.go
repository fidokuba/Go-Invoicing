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

// Create inserts a new organisation row. created_at and updated_at are left
// to PostgreSQL's DEFAULT NOW(), and deleted_at stays NULL until the
// organisation is soft-deleted.
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
	`

	_, err := r.db.Exec(
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
	)
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
			deleted_at
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
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrganisationNotFound
		}

		return nil, fmt.Errorf("get organisation by id: %w", err)
	}

	return &organisation, nil
}

// Update persists every mutable party-detail field in one statement.
// Logo is intentionally absent from the SET list, so it is never touched
// here — see OrganisationRepository.Update's own comment. updated_at is
// bumped explicitly (Create leaves it to the column default, but this is
// a genuine change, not an initial insert).
func (r *PostgresOrganisationRepository) Update(
	ctx context.Context,
	organisationID uuid.UUID,
	organisation *Organisation,
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
			updated_at  = NOW()
		WHERE id = $11
			AND deleted_at IS NULL
	`

	tag, err := r.db.Exec(
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
	)
	if err != nil {
		return fmt.Errorf("update organisation: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrOrganisationNotFound
	}

	return nil
}
