package customer

import (
	"context"

	"github.com/google/uuid"
)

// CustomerRepository describes how customers are read from and written to
// storage. GetByID is organisation-scoped: a customer can only be fetched
// through the organisation it belongs to, so one organisation's data can
// never leak into another's lookup.
type CustomerRepository interface {
	Create(ctx context.Context, customer *Customer) error
	GetByID(ctx context.Context, organisationID uuid.UUID, customerID uuid.UUID) (*Customer, error)
}
