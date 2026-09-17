package invoice

import (
	"strings"
	"time"
)

// CreatePaymentHTTPRequest is the wire shape a client POSTs to
// /invoices/{id}/payments.
//
// It is a distinct type from CreatePaymentRequest (InvoiceService.
// CreatePayment's own input), rather than reusing that name the way
// CreateInvoiceRequest is reused directly by InvoiceService.Create. That's
// forced by an earlier design decision: CreatePaymentRequest.PaymentDate
// is already a real time.Time, not a wire string — chosen when
// CreatePayment was built, before this handler existed, specifically so a
// future handler would parse the date itself (see that type's own doc
// comment). toCreatePaymentRequest below is that parsing step.
//
// Reference and Notes are optional: an empty string converts to nil via
// nilIfEmpty inside InvoiceService.CreatePayment, matching every other
// optional string field in this project.
type CreatePaymentHTTPRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"paymentMethod"`
	PaymentDate   string `json:"paymentDate"`
	Reference     string `json:"reference"`
	Notes         string `json:"notes"`
}

// toCreatePaymentRequest converts the wire request into
// InvoiceService.CreatePayment's input type, parsing PaymentDate
// ("YYYY-MM-DD", the same dateLayout CreateInvoiceRequest's dates use) if
// one was supplied. An empty/blank PaymentDate is left as the zero
// time.Time rather than defaulted here — CreatePayment already defaults a
// zero PaymentDate to time.Now() itself, so this function doesn't
// duplicate that rule.
//
// This is a date-format parsing step, not a business-validation one — the
// same category of thing uuid.Parse(r.PathValue("id")) already does in
// the handler. The actual payment business rules (amount > 0, amount <=
// outstanding) are untouched and still enforced entirely inside
// InvoiceService.CreatePayment.
func (r CreatePaymentHTTPRequest) toCreatePaymentRequest() (CreatePaymentRequest, error) {
	var paymentDate time.Time

	if trimmed := strings.TrimSpace(r.PaymentDate); trimmed != "" {
		parsed, err := time.Parse(dateLayout, trimmed)
		if err != nil {
			return CreatePaymentRequest{}, err
		}

		paymentDate = parsed
	}

	return CreatePaymentRequest{
		Amount:        r.Amount,
		PaymentMethod: r.PaymentMethod,
		PaymentDate:   paymentDate,
		Reference:     r.Reference,
		Notes:         r.Notes,
	}, nil
}

// PaymentResponse is the shape returned to clients for a payment.
type PaymentResponse struct {
	ID            string  `json:"id"`
	InvoiceID     string  `json:"invoiceId"`
	Amount        int64   `json:"amount"`
	PaymentMethod string  `json:"paymentMethod"`
	PaymentDate   string  `json:"paymentDate"`
	Reference     *string `json:"reference,omitempty"`
	Notes         *string `json:"notes,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

// toPaymentResponse maps the internal domain model onto the API's
// response shape.
func toPaymentResponse(p *Payment) PaymentResponse {
	return PaymentResponse{
		ID:            p.ID.String(),
		InvoiceID:     p.InvoiceID.String(),
		Amount:        p.Amount,
		PaymentMethod: p.PaymentMethod,
		PaymentDate:   p.PaymentDate.Format(dateLayout),
		Reference:     p.Reference,
		Notes:         p.Notes,
		CreatedAt:     p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     p.UpdatedAt.Format(time.RFC3339),
	}
}
