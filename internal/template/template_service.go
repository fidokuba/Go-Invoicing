package template

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// maxDefinitionBytes bounds a template's stored definition — generous
// for even a large, heavily-decorated invoice layout, but not unbounded.
// Mirrors the "bound every input" ethos httpx.MaxRequestBodyBytes already
// applies at the whole-request level; this is the one field within a
// template request that could otherwise grow arbitrarily.
const maxDefinitionBytes = 200 * 1024 // 200 KiB

// ErrTemplateDefinitionTooLarge is returned by Create/Update when
// Definition exceeds maxDefinitionBytes.
var ErrTemplateDefinitionTooLarge = fmt.Errorf("template definition must be %d bytes or smaller", maxDefinitionBytes)

// TxBeginner starts a new transaction — the same package-local pattern
// admin.TxBeginner/invoice.TxBeginner already use. *pgxpool.Pool
// satisfies this directly.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// TemplateService owns every business rule this package has: name/
// definition validation, and — the two rules that actually need a
// transaction — that the system (Classic) template can never be
// deleted or edited, and that deleting the current default always
// leaves the organisation's default pointing at Classic, never at
// nothing.
type TemplateService struct {
	repository TemplateRepository
	txBeginner TxBeginner
}

func NewTemplateService(repository TemplateRepository, txBeginner TxBeginner) *TemplateService {
	return &TemplateService{repository: repository, txBeginner: txBeginner}
}

func validateNameAndDefinition(name string, definition json.RawMessage) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrTemplateNameRequired
	}
	if len(definition) > maxDefinitionBytes {
		return "", ErrTemplateDefinitionTooLarge
	}
	return name, nil
}

// Create saves a new, non-default, non-system template. There is no way
// to create a template that is already the organisation's default or
// already a system template — the first is a separate SetDefault call
// ("Use This Layout"), and the second can never be requested at all
// (IsSystem has no setter anywhere in this package's public API).
func (s *TemplateService) Create(ctx context.Context, organisationID uuid.UUID, name string, definition json.RawMessage) (*Template, error) {
	name, err := validateNameAndDefinition(name, definition)
	if err != nil {
		return nil, err
	}

	t := &Template{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           name,
		Definition:     definition,
		IsDefault:      false,
		IsSystem:       false,
	}

	if err := s.repository.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}

	return t, nil
}

func (s *TemplateService) List(ctx context.Context, organisationID uuid.UUID) ([]*Template, error) {
	templates, err := s.repository.List(ctx, organisationID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	return templates, nil
}

func (s *TemplateService) GetByID(ctx context.Context, organisationID, id uuid.UUID) (*Template, error) {
	return s.repository.GetByID(ctx, organisationID, id)
}

// Update changes Name/Definition on an existing, non-system template.
// The system (Classic) template is read-only through this path — not
// just non-deletable — so it always remains a trustworthy fallback: if
// it could be silently edited, "revert to Classic" (see Delete) would no
// longer mean "revert to something known-good."
func (s *TemplateService) Update(ctx context.Context, organisationID, id uuid.UUID, name string, definition json.RawMessage, expectedVersion int64) (*Template, error) {
	name, err := validateNameAndDefinition(name, definition)
	if err != nil {
		return nil, err
	}

	existing, err := s.repository.GetByID(ctx, organisationID, id)
	if err != nil {
		return nil, err
	}
	if existing.IsSystem {
		return nil, ErrTemplateIsSystemCannotBeDeleted
	}

	t := &Template{ID: id, OrganisationID: organisationID, Name: name, Definition: definition}
	if err := s.repository.Update(ctx, organisationID, t, expectedVersion); err != nil {
		return nil, err
	}

	// Update only writes Name/Definition/Version/UpdatedAt — round-trip
	// the rest from what GetByID already told us, rather than a second
	// query, the same shortcut PostgresSettingsRepository.Update's own
	// callers rely on.
	t.IsDefault = existing.IsDefault
	t.IsSystem = existing.IsSystem
	t.CreatedAt = existing.CreatedAt

	return t, nil
}

// Delete removes a template. Two rules, both enforced here rather than
// left to the repository (see TemplateRepository's own doc comment):
//
//   - the system (Classic) template can never be deleted — checked
//     before any write, so a caller gets ErrTemplateIsSystemCannotBeDeleted
//     with nothing changed, not a partial failure;
//   - deleting the organisation's current default template reverts the
//     default to Classic, atomically with the delete itself — the
//     organisation is never left with zero default templates, even for
//     the instant between two separate requests.
func (s *TemplateService) Delete(ctx context.Context, organisationID, id uuid.UUID) error {
	tx, err := s.txBeginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete-template transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := s.repository.WithTx(tx)

	existing, err := repo.GetByID(ctx, organisationID, id)
	if err != nil {
		return err
	}
	if existing.IsSystem {
		return ErrTemplateIsSystemCannotBeDeleted
	}

	if err := repo.SoftDelete(ctx, organisationID, id); err != nil {
		return err
	}

	if existing.IsDefault {
		systemTemplate, err := repo.GetSystemTemplate(ctx, organisationID)
		if err != nil {
			return fmt.Errorf("find classic template to revert to: %w", err)
		}
		if err := repo.SetDefault(ctx, organisationID, systemTemplate.ID); err != nil {
			return fmt.Errorf("revert default to classic template: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete-template transaction: %w", err)
	}

	return nil
}

// SetDefault makes id ("Use This Layout") the organisation's default
// template. Any non-deleted template may become the default, including
// the system Classic one (that's how an organisation returns to Classic
// deliberately, not only via Delete's automatic fallback).
func (s *TemplateService) SetDefault(ctx context.Context, organisationID, id uuid.UUID) error {
	if _, err := s.repository.GetByID(ctx, organisationID, id); err != nil {
		return err
	}

	if err := s.repository.SetDefault(ctx, organisationID, id); err != nil {
		return fmt.Errorf("set default template: %w", err)
	}

	return nil
}
