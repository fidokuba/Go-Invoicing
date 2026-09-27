package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dbExecutor is the same duck-typed abstraction
// internal/administration's own repositories use, re-declared here
// rather than imported — this package-local repetition is the
// established pattern in this codebase (see internal/administration's
// etag.go for the same "deliberately local, nothing else uses it"
// reasoning applied to a different concern), not an oversight.
type dbExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// classicDefinitionPlaceholder mirrors migration 000020_create_invoice_templates
// .up.sql's own literal exactly — the seeded Classic template's Definition
// isn't yet a real template document (the renderer still serves Classic
// via its own hardcoded HTML — see renderer/src/classicTemplate.js), so
// this is a marker, not something any consumer currently parses.
var classicDefinitionPlaceholder = json.RawMessage(`{"system":"classic"}`)

// CreateSystemTemplate creates organisationID's permanent Classic system
// template using tx directly. It is a package-level function, not a
// TemplateRepository method — specifically so it can be assigned
// directly to admin.RegistrationService's TemplateProvisioner field
// (see that field's own doc comment for why this package can't import
// internal/administration's types back here — it already imports that
// package the other way, for RequireAuthenticatedUser). Called from
// RegistrationService.Register's own transaction, so the organisation,
// its settings, its first user, and its Classic template all succeed or
// fail together — no new organisation is ever left without one.
func CreateSystemTemplate(ctx context.Context, tx pgx.Tx, organisationID uuid.UUID) error {
	const query = `
		INSERT INTO invoice_templates (id, organisation_id, name, definition, is_default, is_system)
		VALUES ($1, $2, $3, $4, TRUE, TRUE)
	`

	_, err := tx.Exec(ctx, query, uuid.New(), organisationID, ClassicTemplateName, []byte(classicDefinitionPlaceholder))
	if err != nil {
		return fmt.Errorf("create system template: %w", err)
	}

	return nil
}

// PostgresTemplateRepository is the PostgreSQL-backed implementation of
// TemplateRepository.
type PostgresTemplateRepository struct {
	db dbExecutor
}

func NewPostgresTemplateRepository(db *pgxpool.Pool) *PostgresTemplateRepository {
	return &PostgresTemplateRepository{db: db}
}

func (r *PostgresTemplateRepository) WithTx(tx pgx.Tx) TemplateRepository {
	return &PostgresTemplateRepository{db: tx}
}

func (r *PostgresTemplateRepository) Create(ctx context.Context, t *Template) error {
	const query = `
		INSERT INTO invoice_templates (
			id, organisation_id, name, definition, is_default, is_system
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at, version
	`

	err := r.db.QueryRow(
		ctx, query,
		t.ID, t.OrganisationID, t.Name, []byte(t.Definition), t.IsDefault, t.IsSystem,
	).Scan(&t.CreatedAt, &t.UpdatedAt, &t.Version)
	if err != nil {
		return fmt.Errorf("create template: %w", err)
	}

	return nil
}

func (r *PostgresTemplateRepository) GetByID(ctx context.Context, organisationID, id uuid.UUID) (*Template, error) {
	const query = `
		SELECT id, organisation_id, name, definition, is_default, is_system,
			created_at, updated_at, deleted_at, version
		FROM invoice_templates
		WHERE organisation_id = $1 AND id = $2 AND deleted_at IS NULL
	`

	return r.scanOne(ctx, query, organisationID, id)
}

func (r *PostgresTemplateRepository) GetSystemTemplate(ctx context.Context, organisationID uuid.UUID) (*Template, error) {
	const query = `
		SELECT id, organisation_id, name, definition, is_default, is_system,
			created_at, updated_at, deleted_at, version
		FROM invoice_templates
		WHERE organisation_id = $1 AND is_system = TRUE AND deleted_at IS NULL
	`

	return r.scanOne(ctx, query, organisationID)
}

func (r *PostgresTemplateRepository) GetDefault(ctx context.Context, organisationID uuid.UUID) (*Template, error) {
	const query = `
		SELECT id, organisation_id, name, definition, is_default, is_system,
			created_at, updated_at, deleted_at, version
		FROM invoice_templates
		WHERE organisation_id = $1 AND is_default = TRUE AND deleted_at IS NULL
	`

	return r.scanOne(ctx, query, organisationID)
}

func (r *PostgresTemplateRepository) scanOne(ctx context.Context, query string, args ...any) (*Template, error) {
	var t Template
	var definition []byte

	err := r.db.QueryRow(ctx, query, args...).Scan(
		&t.ID, &t.OrganisationID, &t.Name, &definition, &t.IsDefault, &t.IsSystem,
		&t.CreatedAt, &t.UpdatedAt, &t.DeletedAt, &t.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTemplateNotFound
		}
		return nil, fmt.Errorf("get template: %w", err)
	}

	t.Definition = json.RawMessage(definition)
	return &t, nil
}

func (r *PostgresTemplateRepository) List(ctx context.Context, organisationID uuid.UUID) ([]*Template, error) {
	const query = `
		SELECT id, organisation_id, name, definition, is_default, is_system,
			created_at, updated_at, deleted_at, version
		FROM invoice_templates
		WHERE organisation_id = $1 AND deleted_at IS NULL
		ORDER BY is_system DESC, created_at ASC
	`

	rows, err := r.db.Query(ctx, query, organisationID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		var t Template
		var definition []byte

		if err := rows.Scan(
			&t.ID, &t.OrganisationID, &t.Name, &definition, &t.IsDefault, &t.IsSystem,
			&t.CreatedAt, &t.UpdatedAt, &t.DeletedAt, &t.Version,
		); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}

		t.Definition = json.RawMessage(definition)
		templates = append(templates, &t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read templates: %w", err)
	}

	return templates, nil
}

func (r *PostgresTemplateRepository) Update(ctx context.Context, organisationID uuid.UUID, t *Template, expectedVersion int64) error {
	const query = `
		UPDATE invoice_templates
		SET name       = $1,
			definition = $2,
			updated_at = NOW(),
			version    = version + 1
		WHERE organisation_id = $3
			AND id = $4
			AND deleted_at IS NULL
			AND version = $5
		RETURNING version, updated_at
	`

	err := r.db.QueryRow(
		ctx, query,
		t.Name, []byte(t.Definition), organisationID, t.ID, expectedVersion,
	).Scan(&t.Version, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return r.missingOrStale(ctx, organisationID, t.ID)
		}
		return fmt.Errorf("update template: %w", err)
	}

	return nil
}

// missingOrStale mirrors PostgresSettingsRepository's own helper of the
// same name and reasoning: an Update matching no row is ambiguous
// between "doesn't exist" and "stale version" until checked separately.
func (r *PostgresTemplateRepository) missingOrStale(ctx context.Context, organisationID, id uuid.UUID) error {
	const query = `SELECT EXISTS (SELECT 1 FROM invoice_templates WHERE organisation_id = $1 AND id = $2 AND deleted_at IS NULL)`

	var exists bool
	if err := r.db.QueryRow(ctx, query, organisationID, id).Scan(&exists); err != nil {
		return fmt.Errorf("check template exists: %w", err)
	}

	if exists {
		return ErrTemplateVersionConflict
	}
	return ErrTemplateNotFound
}

func (r *PostgresTemplateRepository) SoftDelete(ctx context.Context, organisationID, id uuid.UUID) error {
	const query = `
		UPDATE invoice_templates
		SET deleted_at = NOW()
		WHERE organisation_id = $1 AND id = $2 AND deleted_at IS NULL
	`

	tag, err := r.db.Exec(ctx, query, organisationID, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTemplateNotFound
	}

	return nil
}

// SetDefault is two UPDATEs, not a read-modify-write: unset whatever
// currently holds is_default for this organisation, then set it on id.
// Doing it in this order means there is never a moment with two TRUE
// rows for the same organisation, so invoice_templates_one_default_per_org
// (a partial unique index, checked per-statement, not deferred) is never
// at risk of a transient violation.
func (r *PostgresTemplateRepository) SetDefault(ctx context.Context, organisationID, id uuid.UUID) error {
	const unset = `
		UPDATE invoice_templates
		SET is_default = FALSE, updated_at = NOW()
		WHERE organisation_id = $1 AND is_default = TRUE AND deleted_at IS NULL
	`
	if _, err := r.db.Exec(ctx, unset, organisationID); err != nil {
		return fmt.Errorf("unset previous default template: %w", err)
	}

	const set = `
		UPDATE invoice_templates
		SET is_default = TRUE, updated_at = NOW()
		WHERE organisation_id = $1 AND id = $2 AND deleted_at IS NULL
	`
	tag, err := r.db.Exec(ctx, set, organisationID, id)
	if err != nil {
		return fmt.Errorf("set default template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTemplateNotFound
	}

	return nil
}
