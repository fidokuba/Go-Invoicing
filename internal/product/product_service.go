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

// ErrProductVATRateInvalid is returned when Create or Update is called
// with a VAT rate outside 0-100%.
var ErrProductVATRateInvalid = errors.New("product VAT rate must be between 0 and 100")

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

// Create validates the requested name, SKU, price and VAT rate,
// generates the product's ID, and persists it under the given
// organisation. Price must be non-negative and is stored exactly as
// given, in integer minor units — no float conversion, no currency
// handling. Optional fields left blank are stored as NULL rather than
// empty strings. New products start active.
func (s *ProductService) Create(
	ctx context.Context,
	organisationID uuid.UUID,
	name string,
	description string,
	sku string,
	price int64,
	category string,
	vatRate *float64,
) (*Product, error) {
	name, sku, err := validateProductFields(name, sku, price, vatRate)
	if err != nil {
		return nil, err
	}

	p := &Product{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           name,
		Description:    nilIfEmpty(description),
		SKU:            sku,
		Price:          price,
		Category:       nilIfEmpty(category),
		VATRate:        vatRate,
		IsActive:       true,
	}

	if err := s.repository.Create(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// Update replaces productID's editable fields — everything but its ID
// and organisation — with the same validation Create applies. isActive
// nil leaves the product's active status unchanged.
func (s *ProductService) Update(
	ctx context.Context,
	organisationID uuid.UUID,
	productID uuid.UUID,
	name string,
	description string,
	sku string,
	price int64,
	category string,
	vatRate *float64,
	isActive *bool,
) (*Product, error) {
	name, sku, err := validateProductFields(name, sku, price, vatRate)
	if err != nil {
		return nil, err
	}

	p, err := s.repository.GetByID(ctx, organisationID, productID)
	if err != nil {
		return nil, err
	}

	p.Name = name
	p.Description = nilIfEmpty(description)
	p.SKU = sku
	p.Price = price
	p.Category = nilIfEmpty(category)
	p.VATRate = vatRate
	if isActive != nil {
		p.IsActive = *isActive
	}

	if err := s.repository.Update(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// validateProductFields applies the rules Create and Update share,
// returning name and sku trimmed.
func validateProductFields(name, sku string, price int64, vatRate *float64) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", ErrProductNameRequired
	}

	sku = strings.TrimSpace(sku)
	if sku == "" {
		return "", "", ErrProductSKURequired
	}

	if price < 0 {
		return "", "", ErrProductPriceNegative
	}

	if vatRate != nil && (*vatRate < 0 || *vatRate > 100) {
		return "", "", ErrProductVATRateInvalid
	}

	return name, sku, nil
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
