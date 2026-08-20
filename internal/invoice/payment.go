package invoice

import "time"

type Payment struct {
	ID            uint      `gorm:"primaryKey"`
	InvoiceID     uint      `gorm:"index"`
	Amount        float64
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
