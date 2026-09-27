package template

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TemplateRepository is the tenant-scoped persistence boundary for
// Template. Every method takes organisationID explicitly (never inferred
// from the template's own row) — the same tenant-isolation discipline
// UserRepository/CustomerRepository already follow, so a lookup can never
// accidentally cross into another organisation's templates.
//
// It deliberately holds no business rules (no "is this the system
// template" branching, no default-reversion logic) — that's
// TemplateService's job, orchestrating these mechanical operations
// inside its own transaction. See TemplateService.Delete for why
// SoftDelete and SetDefault are separate calls rather than one that
// tries to do both.
type TemplateRepository interface {
	// WithTx returns a repository that runs its operations against tx
	// instead of the pool — see TemplateService.Delete and .SetDefault,
	// both of which need more than one write to succeed or fail together.
	WithTx(tx pgx.Tx) TemplateRepository

	Create(ctx context.Context, template *Template) error

	// GetByID fetches a single, non-deleted template. Returns
	// ErrTemplateNotFound both when no row exists and when one exists but
	// belongs to a different organisation — indistinguishable to the
	// caller, on purpose.
	GetByID(ctx context.Context, organisationID, id uuid.UUID) (*Template, error)

	// GetSystemTemplate fetches the organisation's one permanent Classic
	// row (IsSystem true). Every organisation has exactly one, from the
	// moment it exists (migration 000020's backfill, or
	// RegistrationService.Register for one created after) — a missing
	// system template is an ErrTemplateNotFound a caller should treat as
	// a genuine invariant violation, not a normal 404.
	GetSystemTemplate(ctx context.Context, organisationID uuid.UUID) (*Template, error)

	// GetDefault fetches the organisation's one current default template
	// (IsDefault true) — Classic itself when nothing else has been
	// selected. Used by InvoicePDFService (a Draft invoice's PDF always
	// reflects the organisation's *current* default) and
	// InvoiceService.Send (captured once into the invoice's own
	// TemplateSnapshot at the Draft -> Sent transition). Every
	// organisation has exactly one default from the moment it exists,
	// the same invariant GetSystemTemplate's own doc comment describes.
	GetDefault(ctx context.Context, organisationID uuid.UUID) (*Template, error)

	// List returns every non-deleted template for organisationID, system
	// template first, then by CreatedAt.
	List(ctx context.Context, organisationID uuid.UUID) ([]*Template, error)

	// Update persists Name/Definition only — never IsDefault or IsSystem,
	// which have their own dedicated operations below — guarded by
	// expectedVersion exactly like PostgresSettingsRepository.Update:
	// ErrTemplateVersionConflict when a row exists but its version has
	// moved on, ErrTemplateNotFound when it doesn't exist at all.
	Update(ctx context.Context, organisationID uuid.UUID, template *Template, expectedVersion int64) error

	// SoftDelete marks a template deleted. It does not check IsSystem —
	// TemplateService.Delete already refused before this is ever called —
	// and it does not adjust IsDefault on any other row; if the deleted
	// template was the organisation's default, reverting to Classic is a
	// separate SetDefault call the service makes in the same transaction.
	SoftDelete(ctx context.Context, organisationID, id uuid.UUID) error

	// SetDefault atomically makes id the organisation's one default
	// template, unsetting whichever template (if any) held that status
	// before — two UPDATEs, not a read-modify-write, so it can never
	// transiently violate invoice_templates_one_default_per_org.
	SetDefault(ctx context.Context, organisationID, id uuid.UUID) error
}
