package invoice

import (
	"time"

	"github.com/google/uuid"
)

type Payment struct {
	ID            uuid.UUID `gorm:"primaryKey"`
	InvoiceID     uuid.UUID `gorm:"index"`
	Amount        int64
	PaymentMethod string // cash, credit_card, bank_transfer, check
	PaymentDate   time.Time
	Reference     string
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (p *Payment) TableName() string {
	return "payments"
}
