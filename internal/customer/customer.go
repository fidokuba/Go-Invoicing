package customer

import (
	"time"

	"github.com/google/uuid"
)

// Customer status values (Milestone 8 Part 3). Named constants rather
// than scattered string literals, matching this project's existing
// convention for status-like fields (see invoice.InvoiceStatus*) — the
// customers.status column has no CHECK constraint, but the application
// layer (Create, and now the list endpoint's ?status= filter) should
// still only ever produce or accept one of these three values.
const (
	CustomerStatusActive   = "active"
	CustomerStatusInactive = "inactive"
	CustomerStatusArchived = "archived"
)

// Email, Phone, CompanyName and TaxID are pointers because those columns
// are nullable in the customers table; ID, OrganisationID, Name and Status
// are NOT NULL. DeletedAt is a pointer and stays nil until the customer is
// soft-deleted.
type Customer struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	Name           string
	Email          *string
	Phone          *string
	CompanyName    *string
	TaxID          *string
	Status         string // active, inactive, archived
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

func (c *Customer) TableName() string {
	return "customers"
}

func (c *Customer) IsActive() bool {
	return c.Status == "active"
}
