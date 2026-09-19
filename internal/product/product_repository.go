package product

import (
	"context"

	"github.com/google/uuid"
)

// ListFilter narrows GET /products (Milestone 8 Part 3) to a specific,
// fixed set of query capabilities. Search is matched via ILIKE against
// name and sku — see PostgresProductRepository.List for why '%'/'_'
// remain live wildcard characters in the search term. IsActive, when
// non-nil, filters on the products.is_active column exactly — there is
// no separate "status" field on this domain model, unlike Customer.
//
// Sort is a public field name already validated against a repository-
// known allow-list (see httpx.ParseSortOrder); the repository maps it
// onto an actual SQL column via its own explicit switch. Order is "asc"
// or "desc".
type ListFilter struct {
	Search   string
	IsActive *bool
	Sort     string
	Order    string
	Limit    int
	Offset   int
}

// ProductRepository describes how products are read from and written to
// storage. GetByID is organisation-scoped: a product can only be fetched
// through the organisation it belongs to, so one organisation's data can
// never leak into another's lookup.
type ProductRepository interface {
	Create(ctx context.Context, product *Product) error
	GetByID(ctx context.Context, organisationID uuid.UUID, productID uuid.UUID) (*Product, error)

	// List returns the page of products matching filter, tenant-scoped
	// to organisationID, together with the total count of products
	// matching the same filters (ignoring Limit/Offset).
	List(ctx context.Context, organisationID uuid.UUID, filter ListFilter) ([]*Product, int64, error)
}
