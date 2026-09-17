package invoice

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrPaymentAmountInvalid is returned when a payment's amount is not
// strictly greater than zero.
var ErrPaymentAmountInvalid = errors.New("payment amount must be greater than zero")

// Reference and Notes are pointers because those columns are nullable in
// the payments table; every other field here is NOT NULL. There is no
// DeletedAt field because the payments table has no deleted_at column —
// unlike Invoice, payments are not soft-deletable.
//
// Amount is stored in integer minor currency units (e.g. cents), matching
// the BIGINT column and the convention used everywhere else in this
// project for money (Invoice.Subtotal/VATTotal/Total, Line.UnitPrice) —
// never a float.
type Payment struct {
	ID            uuid.UUID
	InvoiceID     uuid.UUID
	Amount        int64
	PaymentMethod string // cash, credit_card, bank_transfer, check
	PaymentDate   time.Time
	Reference     *string
	Notes         *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (p *Payment) TableName() string {
	return "payments"
}

// Validate applies the payment domain's own business rules, independent
// of storage or the HTTP layer — there is no PaymentService yet, so this
// is where "amount must be strictly greater than zero" lives until one
// exists to call it.
func (p *Payment) Validate() error {
	if p.Amount <= 0 {
		return ErrPaymentAmountInvalid
	}

	return nil
}
