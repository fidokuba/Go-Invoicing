package product

import (
	"time"

	"github.com/google/uuid"
)

// Description and Category are pointers because those columns are nullable
// in the products table; ID, OrganisationID, Name, SKU and IsActive are NOT
// NULL. DeletedAt is a pointer and stays nil until the product is
// soft-deleted.
//
// Price is stored in integer minor units (e.g. cents), matching the
// column's BIGINT type and the convention already used for money elsewhere
// in the schema (Invoice.Subtotal, Line.UnitPrice, Payment.Amount) — never
// a float.
type Product struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	Name           string
	Description    *string
	SKU            string
	Price          int64
	Category       *string
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

func (p *Product) TableName() string {
	return "products"
}

func (p *Product) IsAvailable() bool {
	return p.IsActive
}
