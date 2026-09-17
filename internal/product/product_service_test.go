package product

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// fakeProductRepository is an in-memory ProductRepository used to test the
// service/handler without touching PostgreSQL. It mirrors the
// organisation-scoping behaviour of PostgresProductRepository.GetByID so
// tests exercise that behaviour too.
type fakeProductRepository struct {
	products  map[uuid.UUID]Product
	createErr error
}

func newFakeProductRepository() *fakeProductRepository {
	return &fakeProductRepository{
		products: make(map[uuid.UUID]Product),
	}
}

func (f *fakeProductRepository) Create(ctx context.Context, p *Product) error {
	if f.createErr != nil {
		return f.createErr
	}

	f.products[p.ID] = *p
	return nil
}

func (f *fakeProductRepository) GetByID(ctx context.Context, organisationID, productID uuid.UUID) (*Product, error) {
	p, ok := f.products[productID]
	if !ok || p.OrganisationID != organisationID {
		return nil, ErrProductNotFound
	}

	return &p, nil
}

func TestProductService_Create_MissingName(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	_, err := service.Create(context.Background(), uuid.New(), "   ", "", "SKU-1", 1000, "")

	if !errors.Is(err, ErrProductNameRequired) {
		t.Fatalf("expected ErrProductNameRequired, got %v", err)
	}
}

func TestProductService_Create_MissingSKU(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	_, err := service.Create(context.Background(), uuid.New(), "Widget", "", "   ", 1000, "")

	if !errors.Is(err, ErrProductSKURequired) {
		t.Fatalf("expected ErrProductSKURequired, got %v", err)
	}
}

func TestProductService_Create_NegativePrice(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	_, err := service.Create(context.Background(), uuid.New(), "Widget", "", "SKU-1", -1, "")

	if !errors.Is(err, ErrProductPriceNegative) {
		t.Fatalf("expected ErrProductPriceNegative, got %v", err)
	}
}

func TestProductService_Create_Success(t *testing.T) {
	organisationID := uuid.New()
	service := NewProductService(newFakeProductRepository())

	p, err := service.Create(
		context.Background(),
		organisationID,
		"  Widget  ",
		"A very useful widget",
		"  SKU-1  ",
		1999,
		"",
	)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	if p.ID == uuid.Nil {
		t.Error("expected a generated ID, got the nil UUID")
	}

	if p.OrganisationID != organisationID {
		t.Errorf("expected organisation ID %v, got %v", organisationID, p.OrganisationID)
	}

	if p.Name != "Widget" {
		t.Errorf("expected name to be trimmed to %q, got %q", "Widget", p.Name)
	}

	if p.SKU != "SKU-1" {
		t.Errorf("expected SKU to be trimmed to %q, got %q", "SKU-1", p.SKU)
	}

	if p.Description == nil || *p.Description != "A very useful widget" {
		t.Errorf("expected description %q, got %v", "A very useful widget", p.Description)
	}

	if p.Category != nil {
		t.Errorf("expected category to be nil, got %v", *p.Category)
	}

	if p.Price != 1999 {
		t.Errorf("expected price 1999, got %d", p.Price)
	}

	if !p.IsActive {
		t.Error("expected new product to be active")
	}
}
