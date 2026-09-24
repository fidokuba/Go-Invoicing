package invoice

import "time"

// dateLayout is the wire format for IssueDate/DueDate — a plain calendar
// date, matching the DATE columns they map onto (no time-of-day or zone).
const dateLayout = "2006-01-02"

// CreateInvoiceLineRequest is one line of a CreateInvoiceRequest. ProductID
// is optional: when nil/blank, this is a custom line with no backing
// product, using the supplied description and unitPrice directly. When
// present, the product is only checked for existence within the
// organisation — its price is never read; unitPrice always comes from the
// request, which is how an invoice line can legitimately override a
// product's current price.
type CreateInvoiceLineRequest struct {
	ProductID   *string `json:"productId,omitempty"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   int64   `json:"unitPrice"`
	VATRate     float64 `json:"vatRate"`
}

// CreateInvoiceRequest is the shape a client may POST to create an
// invoice. It deliberately exposes only what a caller is allowed to set —
// not ID, InvoiceNumber, Subtotal, VATTotal, Total, Status,
// CreatedAt/UpdatedAt/DeletedAt, or any line's VATAmount/Total, all of
// which are server/domain responsibilities.
//
// IssueDate and DueDate are "YYYY-MM-DD" strings.
type CreateInvoiceRequest struct {
	CustomerID string                     `json:"customerId"`
	IssueDate  string                     `json:"issueDate"`
	DueDate    string                     `json:"dueDate"`
	Lines      []CreateInvoiceLineRequest `json:"lines"`
	Notes      string                     `json:"notes"`
}

// InvoiceLineResponse is the shape of one line within InvoiceResponse.
type InvoiceLineResponse struct {
	ID          string  `json:"id"`
	ProductID   *string `json:"productId,omitempty"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   int64   `json:"unitPrice"`
	VATRate     float64 `json:"vatRate"`
	VATAmount   int64   `json:"vatAmount"`
	Total       int64   `json:"total"`
}

// InvoiceResponse is the shape returned to clients. It exposes the public
// invoice fields and its lines, but never DeletedAt.
//
// AmountPaid and AmountOutstanding are not persisted columns — they are
// derived by toInvoiceResponse from the invoice's Total and whatever
// amountPaid its caller supplies (see that function's doc comment).
//
// Status (Milestone 5) is the invoice's EFFECTIVE status — Draft/Sent/
// Overdue/Paid — not necessarily the same as what's persisted in the
// database: a persisted Sent invoice past its due date is reported here
// as "overdue" without the database ever being touched. There is
// deliberately no second "persistedStatus" field; internally
// Invoice.Status continues to mean the persisted value, but nothing in
// this API has asked for that distinction to be externally visible.
//
// SentAt is nil for a Draft invoice and populated the moment Send
// succeeds.
// Currency (Milestone 8 Part 2) is the single 3-letter code every money
// field on this response is denominated in — Subtotal/VATTotal/Total/
// AmountPaid/AmountOutstanding and every line's UnitPrice/VATAmount/
// Total all share it, since an invoice has exactly one currency for its
// entire lifetime. This is deliberately the only snapshot-derived field
// exposed here; the other twenty immutable seller/customer snapshot
// columns stay internal. See toInvoiceResponse's caller for how this
// value is resolved (live Settings.Currency for a Draft invoice, the
// immutable Invoice.Currency snapshot for anything else).
//
// VATRegistered tells clients whether to show VAT at all for this
// invoice. When false every VAT figure is zero and must not be displayed;
// the fields stay on the wire so the response shape never changes.
type InvoiceResponse struct {
	ID                string                `json:"id"`
	OrganisationID    string                `json:"organisationId"`
	CustomerID        string                `json:"customerId"`
	InvoiceNumber     string                `json:"invoiceNumber"`
	IssueDate         string                `json:"issueDate"`
	DueDate           string                `json:"dueDate"`
	Currency          string                `json:"currency"`
	VATRegistered     bool                  `json:"vatRegistered"`
	Subtotal          int64                 `json:"subtotal"`
	VATTotal          int64                 `json:"vatTotal"`
	Total             int64                 `json:"total"`
	AmountPaid        int64                 `json:"amountPaid"`
	AmountOutstanding int64                 `json:"amountOutstanding"`
	Status            string                `json:"status"`
	SentAt            *string               `json:"sentAt,omitempty"`
	Notes             *string               `json:"notes,omitempty"`
	Lines             []InvoiceLineResponse `json:"lines"`
	CreatedAt         string                `json:"createdAt"`
	UpdatedAt         string                `json:"updatedAt"`
}

// InvoiceListItemResponse is the shape of one row in GET /invoices'
// "items" array (Milestone 8 Part 3 amendment) — deliberately a smaller,
// dedicated type rather than InvoiceResponse with an empty Lines: an
// empty array there would mean "this invoice has zero lines", not "lines
// weren't loaded for this representation", which is genuinely ambiguous
// on the wire. This type has no Lines field at all, so that ambiguity
// cannot arise — a client sees no "lines" key in a list row, ever.
//
// It carries every invoice-level field already available without
// fetching line items — the same fields InvoiceResponse has minus Lines,
// Notes and OrganisationID (organisationId isn't included here for the
// same reason it never appears on any other tenant-scoped response: the
// caller already knows its own organisation from having authenticated as
// it, and there is no genuine client need to see it echoed back). Do not
// add joined display fields (e.g. a customer name) speculatively — a
// future frontend can resolve those from its own customer data.
type InvoiceListItemResponse struct {
	ID                string  `json:"id"`
	InvoiceNumber     string  `json:"invoiceNumber"`
	CustomerID        string  `json:"customerId"`
	IssueDate         string  `json:"issueDate"`
	DueDate           string  `json:"dueDate"`
	Status            string  `json:"status"`
	Currency          string  `json:"currency"`
	Subtotal          int64   `json:"subtotal"`
	VATTotal          int64   `json:"vatTotal"`
	Total             int64   `json:"total"`
	AmountPaid        int64   `json:"amountPaid"`
	AmountOutstanding int64   `json:"amountOutstanding"`
	SentAt            *string `json:"sentAt,omitempty"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`
}

// toInvoiceListItemResponse maps one InvoiceService.List result row onto
// the list-item wire shape — the same pure-function contract as
// toInvoiceResponse (no repository access; now and item.Currency are
// both already resolved by the caller/service before this runs).
func toInvoiceListItemResponse(item InvoiceListItem, now time.Time) InvoiceListItemResponse {
	inv := item.Invoice

	var sentAt *string
	if inv.SentAt != nil {
		s := inv.SentAt.UTC().Format(time.RFC3339)
		sentAt = &s
	}

	return InvoiceListItemResponse{
		ID:                inv.ID.String(),
		InvoiceNumber:     inv.InvoiceNumber,
		CustomerID:        inv.CustomerID.String(),
		IssueDate:         inv.IssueDate.Format(dateLayout),
		DueDate:           inv.DueDate.Format(dateLayout),
		Status:            inv.EffectiveStatus(now),
		Currency:          item.Currency,
		Subtotal:          inv.Subtotal,
		VATTotal:          inv.VATTotal,
		Total:             inv.Total,
		AmountPaid:        item.AmountPaid,
		AmountOutstanding: inv.Total - item.AmountPaid,
		SentAt:            sentAt,
		CreatedAt:         inv.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         inv.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// toInvoiceResponse maps the internal domain model onto the API's response
// shape. It performs no database calls: amountPaid is supplied by the
// caller rather than fetched here, and amountOutstanding is simply
// inv.Total - amountPaid — the same formula CreatePayment already uses
// for its own outstanding-balance check, just recomputed rather than
// shared, since it's a single subtraction.
//
// Callers currently pass two different things for amountPaid: Create's
// handler passes the literal 0 (a brand-new invoice cannot have any
// payments yet — no query needed to know that), while GetByID's handler
// passes the amountPaid InvoiceService.GetByID already fetched.
//
// now is used only to derive Status via Invoice.EffectiveStatus — this
// function stays pure/deterministic itself rather than calling
// time.Now(); the HTTP handler supplies the real current time.
//
// currency is resolved by the caller (InvoiceService.Create/GetByID, via
// resolveInvoiceCurrency) before this function ever runs — this stays a
// pure mapping function with no repository access of its own, exactly
// like amountPaid above.
func toInvoiceResponse(inv *Invoice, lines []*Line, amountPaid int64, currency string, now time.Time) InvoiceResponse {
	lineResponses := make([]InvoiceLineResponse, 0, len(lines))

	for _, l := range lines {
		var productID *string
		if l.ProductID != nil {
			s := l.ProductID.String()
			productID = &s
		}

		lineResponses = append(lineResponses, InvoiceLineResponse{
			ID:          l.ID.String(),
			ProductID:   productID,
			Description: l.Description,
			Quantity:    l.Quantity,
			UnitPrice:   l.UnitPrice,
			VATRate:     l.VATRate,
			VATAmount:   l.VATAmount,
			Total:       l.Total,
		})
	}

	var sentAt *string
	if inv.SentAt != nil {
		s := inv.SentAt.UTC().Format(time.RFC3339)
		sentAt = &s
	}

	return InvoiceResponse{
		ID:                inv.ID.String(),
		OrganisationID:    inv.OrganisationID.String(),
		CustomerID:        inv.CustomerID.String(),
		InvoiceNumber:     inv.InvoiceNumber,
		IssueDate:         inv.IssueDate.Format(dateLayout),
		DueDate:           inv.DueDate.Format(dateLayout),
		Currency:          currency,
		VATRegistered:     inv.VATRegistered,
		Subtotal:          inv.Subtotal,
		VATTotal:          inv.VATTotal,
		Total:             inv.Total,
		AmountPaid:        amountPaid,
		AmountOutstanding: inv.Total - amountPaid,
		Status:            inv.EffectiveStatus(now),
		SentAt:            sentAt,
		Notes:             inv.Notes,
		Lines:             lineResponses,
		CreatedAt:         inv.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         inv.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
