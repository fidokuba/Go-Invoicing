package product

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrProductNameRequired is returned when Create is called with an empty
// (or whitespace-only) name.
var ErrProductNameRequired = errors.New("product name is required")

// ErrProductSKURequired is returned when Create is called with an empty
// (or whitespace-only) SKU.
var ErrProductSKURequired = errors.New("product SKU is required")

// ErrProductPriceNegative is returned when Create is called with a
// negative price.
var ErrProductPriceNegative = errors.New("product price cannot be negative")

// ProductService sits between the HTTP layer and the repository. It
// depends on the ProductRepository interface, not on any concrete
// implementation.
type ProductService struct {
	repository ProductRepository
}

func NewProductService(
	repository ProductRepository,
) *ProductService {
	return &ProductService{
		repository: repository,
	}
}

// Create validates the requested name, SKU and price, generates the
// product's ID, and persists it under the given organisation. Price must
// be non-negative and is stored exactly as given, in integer minor units —
// no float conversion, no currency handling. Optional fields left blank
// are stored as NULL rather than empty strings. New products start
// active.
func (s *ProductService) Create(
	ctx context.Context,
	organisationID uuid.UUID,
	name string,
	description string,
	sku string,
	price int64,
	category string,
) (*Product, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrProductNameRequired
	}

	sku = strings.TrimSpace(sku)
	if sku == "" {
		return nil, ErrProductSKURequired
	}

	if price < 0 {
		return nil, ErrProductPriceNegative
	}

	p := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           name,
		Description:    nilIfEmpty(description),
		SKU:            sku,
		Price:          price,
		Category:       nilIfEmpty(category),
		IsActive:       true,
	}

	if err := s.repository.Create(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// GetByID delegates straight to the repository; the organisation scoping
// happens there.
func (s *ProductService) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	productID uuid.UUID,
) (*Product, error) {
	return s.repository.GetByID(ctx, organisationID, productID)
}

// List delegates straight to the repository, which enforces tenant
// scoping and the Search/IsActive/Sort/Order/Limit/Offset predicates in
// SQL. There is no product-specific business validation to apply here —
// unlike Customer's ?status=, IsActive is already a type-safe bool by
// the time it reaches this method, and Sort has already been validated
// against the repository's known public field names by
// httpx.ParseSortOrder before this is ever called.
func (s *ProductService) List(
	ctx context.Context,
	organisationID uuid.UUID,
	filter ListFilter,
) ([]*Product, int64, error) {
	return s.repository.List(ctx, organisationID, filter)
}

// nilIfEmpty converts a blank/whitespace-only string into a nil pointer so
// optional fields are stored as SQL NULL rather than empty strings.
func nilIfEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
