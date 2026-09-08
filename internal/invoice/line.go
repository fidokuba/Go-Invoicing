package invoice

import "time"

type Line struct {
	ID          string `gorm:"primaryKey"`
	InvoiceID   string `gorm:"index"`
	ProductID   string `gorm:"index"`
	Description string
	Quantity    float64
	UnitPrice   int64
	VatRate     float64
	VatAmount   int64
	Total       int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (l *Line) TableName() string {
	return "invoice_lines"
}

func (l *Line) CalculateTotal() int64 {
	return l.Total
}
