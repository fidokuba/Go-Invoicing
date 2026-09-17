package invoice

import (
	"time"

	"github.com/google/uuid"
)

type Line struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	InvoiceID uuid.UUID `gorm:"index"`
	// Nullable: a line can describe free-text work with no product behind it.
	ProductID   *uuid.UUID `gorm:"index"`
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
