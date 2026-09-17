package invoice

import (
	"time"

	"github.com/google/uuid"
)

// VATRate/VATAmount (not VatRate/VatAmount) match Go's convention of
// keeping acronyms upper-cased.
type Line struct {
	ID        uuid.UUID
	InvoiceID uuid.UUID
	// Nullable: a line can describe free-text work with no product behind it.
	ProductID   *uuid.UUID
	Description string
	Quantity    float64
	UnitPrice   int64
	VATRate     float64
	VATAmount   int64
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
