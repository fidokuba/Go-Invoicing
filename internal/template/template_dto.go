package template

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CreateTemplateRequest is the shape a client POSTs to /api/v1/templates.
type CreateTemplateRequest struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
}

// UpdateTemplateRequest is the shape a client PATCHes to
// /api/v1/templates/{id} — the "Save" action. There is no isDefault or
// isSystem field here: those are never set through this route (see
// TemplateHandler.SetDefault for "Use This Layout", and IsSystem has no
// setter anywhere in this package at all).
type UpdateTemplateRequest struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
}

// TemplateResponse is the shape returned for a single template. Version
// is deliberately absent here — like Settings/Organisation, it is
// exposed only as the HTTP ETag (see etag.go), never duplicated into the
// JSON body.
type TemplateResponse struct {
	ID             uuid.UUID       `json:"id"`
	OrganisationID uuid.UUID       `json:"organisationId"`
	Name           string          `json:"name"`
	Definition     json.RawMessage `json:"definition"`
	IsDefault      bool            `json:"isDefault"`
	IsSystem       bool            `json:"isSystem"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func toTemplateResponse(t *Template) TemplateResponse {
	return TemplateResponse{
		ID:             t.ID,
		OrganisationID: t.OrganisationID,
		Name:           t.Name,
		Definition:     t.Definition,
		IsDefault:      t.IsDefault,
		IsSystem:       t.IsSystem,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

// TemplateListResponse is the shape returned for GET /api/v1/templates —
// deliberately not the paginated {items, total, limit, offset} envelope
// customers/products/invoices use: an organisation's template count is
// always small (a handful of saved layouts, not thousands of business
// records), so listing them all in one response needs no pagination.
type TemplateListResponse struct {
	Items []TemplateResponse `json:"items"`
}

func toTemplateListResponse(templates []*Template) TemplateListResponse {
	items := make([]TemplateResponse, len(templates))
	for i, t := range templates {
		items[i] = toTemplateResponse(t)
	}
	return TemplateListResponse{Items: items}
}
