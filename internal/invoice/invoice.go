package invoice

import (
	"time"

	"github.com/google/uuid"
)

// Invoice status values. Named constants rather than scattered string
// literals — now that payment processing has real status-dependent
// business logic (CreatePayment sets InvoiceStatusPaid, IsOverdue checks
// against it), a typo in a literal would silently produce wrong behaviour
// instead of a compile error.
const (
	InvoiceStatusDraft     = "draft"
	InvoiceStatusSent      = "sent"
	InvoiceStatusPaid      = "paid"
	InvoiceStatusOverdue   = "overdue"
	InvoiceStatusCancelled = "cancelled"
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
	Status         string // one of the InvoiceStatus* constants above
	Notes          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

func (i *Invoice) TableName() string {
	return "invoices"
}

func (i *Invoice) IsOverdue() bool {
	return i.Status != InvoiceStatusPaid && time.Now().After(i.DueDate)
}
