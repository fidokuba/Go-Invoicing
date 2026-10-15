package admin

import (
	"time"

	"github.com/google/uuid"
)

// DeletedAt is a pointer because that column is nullable in the settings
// table; every other field here is NOT NULL.
//
// InvoiceNumber represents the LAST allocated invoice number, not the
// next one to use — a brand-new organisation's settings start at 0
// (meaning "none allocated yet"), and NextInvoiceNumber increments before
// returning, so the first call yields 1.
type Settings struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	InvoicePrefix  string // e.g. "INV-"
	InvoiceNumber  int
	Currency       string
	PaymentTerms   int // Days
	// QuantityUnits is the organisation's list of quantity units, offered
	// as the choices for a product's or invoice line's unit.
	QuantityUnits []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time

	// Version (Milestone 13 Part 2) is the optimistic-concurrency token:
	// incremented only by Update (a user-facing edit) — never by invoice
	// number allocation — and exposed only as the HTTP ETag.
	Version int64
}

// DefaultQuantityUnits is the list every organisation starts with —
// the same values migration 000026 gave every existing organisation.
var DefaultQuantityUnits = []string{"Day(s)", "Hour(s)", "KG", "g", "Pcs"}

// MaxQuantityUnitLength bounds one quantity unit, wherever it is stored:
// the settings list, a product, or an invoice line.
const MaxQuantityUnitLength = 30

// MaxQuantityUnits bounds how many units one organisation's list holds.
const MaxQuantityUnits = 50

func (s *Settings) TableName() string {
	return "settings"
}

func (s *Settings) NextInvoiceNumber() int {
	s.InvoiceNumber++
	return s.InvoiceNumber
}
