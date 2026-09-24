package admin

import (
	"time"

	"github.com/google/uuid"
)

// Email, Phone, Website, Logo, Address, City, State, PostalCode, Country and
// TaxID are pointers because those columns are nullable in the organisations
// table; only ID and Name are NOT NULL. DeletedAt is likewise a pointer and
// stays nil until the organisation is soft-deleted.
type Organisation struct {
	ID         uuid.UUID
	Name       string
	Email      *string
	Phone      *string
	Website    *string
	Logo       *string
	Address    *string
	City       *string
	State      *string
	PostalCode *string
	Country    *string
	TaxID      *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time

	// VATRegistered records whether the organisation is registered for
	// UK VAT. When false the organisation may not charge VAT, and neither
	// its TaxID nor any VAT figure appears on its invoices. TaxID is kept
	// while it's false, so ticking the box again restores it.
	VATRegistered bool

	// Version (Milestone 13 Part 2) is the optimistic-concurrency token:
	// incremented on every user-facing Update, exposed only as the HTTP
	// ETag, never in JSON.
	Version int64
}

func (o *Organisation) TableName() string {
	return "organisations"
}
