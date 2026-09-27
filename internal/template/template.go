// Package template is the invoice-template builder's data layer:
// storing, listing, updating, deleting and selecting the templates an
// organisation has saved (Phase 2 of custom invoice layouts — see
// renderer/ and internal/renderer for Phase 1's rendering pipeline,
// which this package's rows will eventually feed). It holds no HTTP
// awareness of its own beyond its handler, and no rendering logic at
// all — Definition is an opaque JSON blob to this package, meaningful
// only to the frontend builder and (in a later phase) the renderer.
package template

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ClassicTemplateName is the seeded, permanent template every
// organisation has from the moment it exists — either backfilled by
// migration 000020 for an organisation that predates this feature, or
// created alongside it by RegistrationService.Register for one created
// after. It is the one template IsSystem is ever true for.
const ClassicTemplateName = "Classic"

var (
	// ErrTemplateNotFound is returned when a lookup finds no matching,
	// non-deleted template in the caller's own organisation — never
	// distinguished from "belongs to a different organisation": both look
	// identical to the caller, the same tenant-isolation reasoning
	// ErrUserNotFound/ErrCustomerNotFound already use elsewhere.
	ErrTemplateNotFound = errors.New("template not found")

	ErrTemplateNameRequired = errors.New("template name is required")

	// ErrTemplateIsSystemCannotBeDeleted is returned when Delete is
	// called against the organisation's permanent Classic template. This
	// is checked before any database write is attempted — see
	// TemplateService.Delete.
	ErrTemplateIsSystemCannotBeDeleted = errors.New("the system default template cannot be deleted")

	// ErrTemplateVersionConflict mirrors ErrSettingsVersionConflict/
	// ErrOrganisationVersionConflict: Update's optimistic-concurrency
	// check found the row had already moved on from the version the
	// caller last read.
	ErrTemplateVersionConflict = errors.New("template has been modified since it was last read")
)

// Template is one saved invoice layout. DeletedAt is a pointer because
// that column is nullable (soft delete); every other field here is NOT
// NULL.
type Template struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	Name           string

	// Definition is the template builder's own document — opaque to this
	// package, stored and returned verbatim, never inspected or
	// validated beyond "is it valid JSON" (TemplateService.validate).
	Definition json.RawMessage

	// IsDefault is true for exactly one non-deleted template per
	// organisation, enforced by invoice_templates_one_default_per_org
	// (a partial unique index) — never by application-level locking.
	IsDefault bool

	// IsSystem is true only for the organisation's permanent Classic
	// template — the one row Delete always refuses, and the one Delete
	// falls back to when the current default is removed. Never true for
	// a user-created template; nothing in this package's API lets a
	// caller set it.
	IsSystem bool

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	// Version is the optimistic-concurrency token, incremented only by
	// Update — the same pattern Settings/Organisation already use,
	// exposed over HTTP only as an ETag (see etag.go).
	Version int64
}

func (t *Template) TableName() string {
	return "invoice_templates"
}
