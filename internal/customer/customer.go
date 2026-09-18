package customer

import (
	"time"

	"github.com/google/uuid"
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
