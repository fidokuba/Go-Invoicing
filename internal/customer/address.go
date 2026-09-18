package customer

import (
	"time"

	"github.com/google/uuid"
)

// AddressTypeBilling is the only address Type this project currently
// creates or reads (Milestone 7 Part 1). The column itself is a plain
// TEXT rather than a constrained enum, matching Status on Customer and
// every other free-text classification column in this schema — "shipping"
// and "other" remain possible future values with no schema change needed,
// but nothing in this codebase writes or reads them yet.
const AddressTypeBilling = "billing"

// Address is a single postal address belonging to a Customer. Street,
// City, State, PostalCode and Country are plain strings even though their
// columns are nullable — AddressRepository.UpsertBillingAddress always
// supplies Street/City/PostalCode/Country (CustomerService validates them
// as required before persisting), and State is optional business-wise but
// needs no NULL-vs-empty distinction the way Organisation's PATCH fields
// do, since this is a whole-resource PUT, not a partial patch.
//
// IsDefault exists on the table but has no reader anywhere in this
// milestone — with exactly one address type (billing) per customer, there
// is nothing yet for "default" to distinguish among.
type Address struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	Type       string
	Street     string
	City       string
	State      string
	PostalCode string
	Country    string
	IsDefault  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
}

func (a *Address) TableName() string {
	return "addresses"
}
