package invoice

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func validPayment() *Payment {
	return &Payment{
		ID:            uuid.New(),
		InvoiceID:     uuid.New(),
		Amount:        1000,
		PaymentMethod: "cash",
	}
}

func TestPayment_Validate_Success(t *testing.T) {
	p := validPayment()

	if err := p.Validate(); err != nil {
		t.Fatalf("expected a valid payment to pass validation, got %v", err)
	}
}

func TestPayment_Validate_ZeroAmount(t *testing.T) {
	p := validPayment()
	p.Amount = 0

	if err := p.Validate(); !errors.Is(err, ErrPaymentAmountInvalid) {
		t.Fatalf("expected ErrPaymentAmountInvalid, got %v", err)
	}
}

func TestPayment_Validate_NegativeAmount(t *testing.T) {
	p := validPayment()
	p.Amount = -1000

	if err := p.Validate(); !errors.Is(err, ErrPaymentAmountInvalid) {
		t.Fatalf("expected ErrPaymentAmountInvalid, got %v", err)
	}
}

func TestPayment_Validate_SmallestValidAmount(t *testing.T) {
	// 1 minor currency unit (e.g. one cent) is the smallest strictly
	// positive amount, and must be accepted.
	p := validPayment()
	p.Amount = 1

	if err := p.Validate(); err != nil {
		t.Fatalf("expected amount of 1 to be valid, got %v", err)
	}
}
