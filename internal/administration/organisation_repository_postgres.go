package admin

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresOrganisationRepository is the PostgreSQL-backed implementation of
// OrganisationRepository. It holds a connection pool rather than creating one
// itself, so the caller decides how the pool is configured and when it is
// closed.
type PostgresOrganisationRepository struct {
	db *pgxpool.Pool
}

// NewPostgresOrganisationRepository wires an existing pool into a repository.
func NewPostgresOrganisationRepository(db *pgxpool.Pool) *PostgresOrganisationRepository {
	return &PostgresOrganisationRepository{
		db: db,
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
		return err
	}

	return nil
}
