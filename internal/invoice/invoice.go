package invoice

import (
	"time"

	"github.com/google/uuid"
)

// Notes is a pointer because that column is nullable in the invoices
// table; every other field here is NOT NULL. DeletedAt is a pointer and
// stays nil until the invoice is soft-deleted.
//
// VATTotal (not VatTotal) matches Go's convention of keeping acronyms
// upper-cased, and matches the field name used elsewhere in this
// milestone's domain/DTO naming.
type Invoice struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	CustomerID     uuid.UUID
	InvoiceNumber  string
	IssueDate      time.Time
	DueDate        time.Time
	Subtotal       int64
	VATTotal       int64
	Total          int64
	Status         string // draft, sent, paid, overdue, cancelled
	Notes          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

func (i *Invoice) TableName() string {
	return "invoices"
}

func (i *Invoice) IsOverdue() bool {
	return i.Status != "paid" && time.Now().After(i.DueDate)
}
