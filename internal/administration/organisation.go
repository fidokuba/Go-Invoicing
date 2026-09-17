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
}

func (o *Organisation) TableName() string {
	return "organisations"
}
