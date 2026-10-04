package admin

import (
	"time"

	"github.com/google/uuid"
)

// Email, Phone, Website, Address, City, State, PostalCode, Country and
// TaxID are pointers because those columns are nullable in the organisations
// table; only ID and Name are NOT NULL. DeletedAt is likewise a pointer and
// stays nil until the organisation is soft-deleted.
type Organisation struct {
	ID         uuid.UUID
	Name       string
	Email      *string
	Phone      *string
	Website    *string
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

	// LogoID is the organisation's current logo (an OrganisationLogo
	// row), or nil for none. Changed only by SaveLogo/ClearLogo — never by
	// Update, and it doesn't bump Version: the logo is its own
	// sub-resource, so replacing it can't conflict with a details edit.
	LogoID *uuid.UUID

	// Version (Milestone 13 Part 2) is the optimistic-concurrency token:
	// incremented on every user-facing Update, exposed only as the HTTP
	// ETag, never in JSON.
	Version int64
}

func (o *Organisation) TableName() string {
	return "organisations"
}
