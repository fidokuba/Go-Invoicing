package product

import (
	"context"

	"github.com/google/uuid"
)

// ProductRepository describes how products are read from and written to
// storage. GetByID is organisation-scoped: a product can only be fetched
// through the organisation it belongs to, so one organisation's data can
// never leak into another's lookup.
type ProductRepository interface {
	Create(ctx context.Context, product *Product) error
	GetByID(ctx context.Context, organisationID uuid.UUID, productID uuid.UUID) (*Product, error)
}
