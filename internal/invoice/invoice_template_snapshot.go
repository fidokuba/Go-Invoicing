package invoice

import (
	"encoding/json"

	"github.com/google/uuid"
)

// TemplateSnapshot is the JSON shape stored in
// Invoice.RenderedTemplateSnapshot — captured exactly once, by MarkSent,
// from the organisation's current default template at the moment of
// Send (see InvoiceService.Send and InvoiceService.buildTemplateSnapshot).
// Migration 000021's own comment has the full immutability reasoning:
// this exists so editing — or even deleting — the template an invoice
// used can never change how that invoice renders afterward.
//
// IsSystem is captured explicitly (not re-derived by re-fetching the
// live template row later) so InvoicePDFService can decide gopdf-vs-
// renderer-service purely from the snapshot, without a second lookup
// that could itself have changed since Send.
type TemplateSnapshot struct {
	TemplateID uuid.UUID       `json:"templateId"`
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
	IsSystem   bool            `json:"isSystem"`
}
