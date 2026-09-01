package invoice

import "time"

type Line struct {
	ID          uint `gorm:"primaryKey"`
	InvoiceID   uint `gorm:"index"`
	ProductID   uint `gorm:"index"`
	Description string
	Quantity    float64
	UnitPrice   float64
	Total       float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (l *Line) TableName() string {
	return "invoice_lines"
}

func (l *Line) CalculateTotal() float64 {
	return l.Quantity * l.UnitPrice
}
