package invoice

import "time"

type Payment struct {
	ID            string `gorm:"primaryKey"`
	InvoiceID     string `gorm:"index"`
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
