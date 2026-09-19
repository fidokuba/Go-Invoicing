package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSettingsNotFound is returned when a lookup finds no matching
// settings row for the given organisation, as opposed to a genuine
// database failure.
var ErrSettingsNotFound = errors.New("settings not found")

// dbExecutor is the minimal query surface this repository needs. Both
// *pgxpool.Pool and pgx.Tx implement it, which is what lets these methods
// run unmodified whether they're operating directly on the pool or inside
// a transaction the caller is managing — the repository doesn't know or
// care which. Same pattern as invoice.dbExecutor.
type dbExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresSettingsRepository is the PostgreSQL-backed implementation of
// SettingsRepository. It holds a connection pool rather than creating one
// itself, so the caller decides how the pool is configured and when it is
// closed.
type PostgresSettingsRepository struct {
	db dbExecutor
}

// NewPostgresSettingsRepository wires an existing pool into a repository.
func NewPostgresSettingsRepository(db *pgxpool.Pool) *PostgresSettingsRepository {
	return &PostgresSettingsRepository{
		db: db,
	}
}

// WithTx returns a repository that runs its operations against tx instead
// of the pool, so settings can be locked and updated within a
// caller-managed transaction. It does not begin, commit or roll back
// anything itself — tx is already open, and finishing it remains the
// caller's responsibility.
func (r *PostgresSettingsRepository) WithTx(tx pgx.Tx) SettingsRepository {
	return &PostgresSettingsRepository{
		db: tx,
	}
}

// Create inserts a new settings row. created_at and updated_at are left to
// PostgreSQL's DEFAULT NOW(), and deleted_at stays NULL until the
// settings are soft-deleted. Every column that has a database default
// (invoice_prefix, invoice_number, currency, payment_terms) is supplied
// explicitly here regardless, so this repository never depends on those
// defaults being what a caller expects.
func (r *PostgresSettingsRepository) Create(
	ctx context.Context,
	settings *Settings,
) error {
	const query = `
		INSERT INTO settings (
			id,
			organisation_id,
			invoice_prefix,
			invoice_number,
			currency,
			payment_terms
		)
		VALUES (
			$1, $2, $3, $4, $5, $6
		)
	`

	_, err := r.db.Exec(
		ctx,
		query,
		settings.ID,
		settings.OrganisationID,
		settings.InvoicePrefix,
		settings.InvoiceNumber,
		settings.Currency,
		settings.PaymentTerms,
	)
	if err != nil {
		return fmt.Errorf("create settings: %w", err)
	}

	return nil
}

// GetByOrganisationID fetches an organisation's settings without locking
// the row. Soft-deleted settings are excluded.
func (r *PostgresSettingsRepository) GetByOrganisationID(
	ctx context.Context,
	organisationID uuid.UUID,
) (*Settings, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			invoice_prefix,
			invoice_number,
			currency,
			payment_terms,
			created_at,
			updated_at,
			deleted_at
		FROM settings
		WHERE organisation_id = $1
			AND deleted_at IS NULL
	`

	return r.scanOne(ctx, query, organisationID)
}

// GetForUpdate fetches an organisation's settings and locks the row with
// FOR UPDATE. The lock is held by PostgreSQL until whichever transaction
// this repository was obtained via WithTx for is committed or rolled
// back — calling this outside of a transaction (i.e. against the bare
// pool) still works, but the lock is released the instant this statement
// finishes, so it provides no protection in that case. Callers doing
// invoice number allocation must always go through WithTx first.
func (r *PostgresSettingsRepository) GetForUpdate(
	ctx context.Context,
	organisationID uuid.UUID,
) (*Settings, error) {
	const query = `
		SELECT
			id,
			organisation_id,
			invoice_prefix,
			invoice_number,
			currency,
			payment_terms,
			created_at,
			updated_at,
			deleted_at
		FROM settings
		WHERE organisation_id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`

	return r.scanOne(ctx, query, organisationID)
}

// scanOne runs a single-row settings query (used by both GetByOrganisationID
// and GetForUpdate, which differ only in their SQL) and translates
// pgx.ErrNoRows into ErrSettingsNotFound.
func (r *PostgresSettingsRepository) scanOne(
	ctx context.Context,
	query string,
	organisationID uuid.UUID,
) (*Settings, error) {
	var s Settings

	err := r.db.QueryRow(ctx, query, organisationID).Scan(
		&s.ID,
		&s.OrganisationID,
		&s.InvoicePrefix,
		&s.InvoiceNumber,
		&s.Currency,
		&s.PaymentTerms,
		&s.CreatedAt,
		&s.UpdatedAt,
		&s.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettingsNotFound
		}

		return nil, fmt.Errorf("get settings: %w", err)
	}

	return &s, nil
}

// Update persists InvoicePrefix, Currency and PaymentTerms in one
// statement, tenant-scoped by organisationID, and scans the resulting
// updated_at back onto settings via RETURNING — matching every other
// Update in this project (see OrganisationRepository.Update). It
// deliberately never touches invoice_number — see SettingsRepository
// .Update's own comment for why that column belongs solely to the
// locked allocate-and-increment sequence.
func (r *PostgresSettingsRepository) Update(
	ctx context.Context,
	organisationID uuid.UUID,
	settings *Settings,
) error {
	const query = `
		UPDATE settings
		SET invoice_prefix = $1,
			currency        = $2,
			payment_terms   = $3,
			updated_at      = NOW()
		WHERE organisation_id = $4
			AND deleted_at IS NULL
		RETURNING updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		settings.InvoicePrefix,
		settings.Currency,
		settings.PaymentTerms,
		organisationID,
	).Scan(&settings.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSettingsNotFound
		}

		return fmt.Errorf("update settings: %w", err)
	}

	return nil
}

// UpdateInvoiceNumber persists a new invoice_number for the organisation.
// Called with the value already incremented by Settings.NextInvoiceNumber,
// through the same transaction that acquired the GetForUpdate lock, so the
// read-increment-write sequence is atomic with respect to other
// transactions doing the same for the same organisation.
func (r *PostgresSettingsRepository) UpdateInvoiceNumber(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceNumber int,
) error {
	const query = `
		UPDATE settings
		SET invoice_number = $1,
			updated_at = NOW()
		WHERE organisation_id = $2
			AND deleted_at IS NULL
	`

	tag, err := r.db.Exec(ctx, query, invoiceNumber, organisationID)
	if err != nil {
		return fmt.Errorf("update settings invoice number: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrSettingsNotFound
	}

	return nil
}
