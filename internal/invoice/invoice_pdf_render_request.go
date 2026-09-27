package invoice

import (
	"encoding/json"

	"go-invoicing/internal/renderer"
)

// ToRenderRequest converts data into the wire format the renderer
// service's POST /render accepts, attaching definition (a saved
// template's own document — nil for the system Classic template, which
// the renderer serves via its own hardcoded HTML instead of
// interpreting this field — see renderer.RenderRequest.Definition).
//
// This conversion lives here, not in internal/renderer, specifically to
// avoid a circular import: this package needs to import
// internal/renderer (InvoicePDFService calls its Client), so
// internal/renderer cannot import this package back — see that
// package's own doc comment.
func (data InvoicePDFData) ToRenderRequest(definition json.RawMessage) renderer.RenderRequest {
	lines := make([]renderer.Line, len(data.Lines))
	for i, l := range data.Lines {
		lines[i] = renderer.Line{
			Description: l.Description,
			Quantity:    l.Quantity,
			UnitPrice:   l.UnitPrice,
			VATRate:     l.VATRate,
			VATAmount:   l.VATAmount,
			Total:       l.Total,
		}
	}

	return renderer.RenderRequest{
		InvoiceNumber: data.InvoiceNumber,
		IssueDate:     data.IssueDate,
		DueDate:       data.DueDate,
		StatusLabel:   data.StatusLabel,
		Seller: renderer.Seller{
			Name:         data.Seller.Name,
			AddressLines: data.Seller.AddressLines,
			Email:        data.Seller.Email,
			Phone:        data.Seller.Phone,
			Website:      data.Seller.Website,
			TaxID:        data.Seller.TaxID,
		},
		Customer: renderer.Customer{
			DisplayName:  data.Customer.DisplayName,
			AddressLines: data.Customer.AddressLines,
			Email:        data.Customer.Email,
			TaxID:        data.Customer.TaxID,
		},
		Lines:              lines,
		VATRegistered:      data.VATRegistered,
		Subtotal:           data.Subtotal,
		VATTotal:           data.VATTotal,
		Total:              data.Total,
		ShowPaymentSummary: data.ShowPaymentSummary,
		AmountPaid:         data.AmountPaid,
		AmountOutstanding:  data.AmountOutstanding,
		Notes:              data.Notes,
		Definition:         definition,
	}
}
