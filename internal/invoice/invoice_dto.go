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
type InvoiceResponse struct {
	ID             string                `json:"id"`
	OrganisationID string                `json:"organisationId"`
	CustomerID     string                `json:"customerId"`
	InvoiceNumber  string                `json:"invoiceNumber"`
	IssueDate      string                `json:"issueDate"`
	DueDate        string                `json:"dueDate"`
	Subtotal       int64                 `json:"subtotal"`
	VATTotal       int64                 `json:"vatTotal"`
	Total          int64                 `json:"total"`
	Status         string                `json:"status"`
	Notes          *string               `json:"notes,omitempty"`
	Lines          []InvoiceLineResponse `json:"lines"`
	CreatedAt      string                `json:"createdAt"`
	UpdatedAt      string                `json:"updatedAt"`
}

// toInvoiceResponse maps the internal domain model onto the API's response
// shape.
func toInvoiceResponse(inv *Invoice, lines []*Line) InvoiceResponse {
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

	return InvoiceResponse{
		ID:             inv.ID.String(),
		OrganisationID: inv.OrganisationID.String(),
		CustomerID:     inv.CustomerID.String(),
		InvoiceNumber:  inv.InvoiceNumber,
		IssueDate:      inv.IssueDate.Format(dateLayout),
		DueDate:        inv.DueDate.Format(dateLayout),
		Subtotal:       inv.Subtotal,
		VATTotal:       inv.VATTotal,
		Total:          inv.Total,
		Status:         inv.Status,
		Notes:          inv.Notes,
		Lines:          lineResponses,
		CreatedAt:      inv.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      inv.UpdatedAt.Format(time.RFC3339),
	}
}
