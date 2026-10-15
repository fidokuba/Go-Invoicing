package product

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeProductRepository is an in-memory ProductRepository used to test the
// service/handler without touching PostgreSQL. It mirrors the
// organisation-scoping behaviour of PostgresProductRepository.GetByID so
// tests exercise that behaviour too.
type fakeProductRepository struct {
	products  map[uuid.UUID]Product
	createErr error
	updateErr error
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

func (f *fakeProductRepository) Update(ctx context.Context, p *Product) error {
	existing, ok := f.products[p.ID]
	if !ok || existing.OrganisationID != p.OrganisationID {
		return ErrProductNotFound
	}
	if f.updateErr != nil {
		return f.updateErr
	}

	p.UpdatedAt = time.Now()
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

// List is a simple in-memory re-implementation of the same filter/sort/
// paginate contract PostgresProductRepository.List implements in SQL —
// good enough for handler-level tests; the real SQL predicates are
// proven separately by product_repository_postgres_test.go.
func (f *fakeProductRepository) List(ctx context.Context, organisationID uuid.UUID, filter ListFilter) ([]*Product, int64, error) {
	var matched []*Product

	for i := range f.products {
		p := f.products[i]
		if p.OrganisationID != organisationID {
			continue
		}
		if filter.IsActive != nil && p.IsActive != *filter.IsActive {
			continue
		}
		if filter.Search != "" {
			needle := strings.ToLower(filter.Search)
			if !strings.Contains(strings.ToLower(p.Name), needle) && !strings.Contains(strings.ToLower(p.SKU), needle) {
				continue
			}
		}
		pCopy := p
		matched = append(matched, &pCopy)
	}

	desc := strings.EqualFold(filter.Order, "desc")

	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]

		switch filter.Sort {
		case "sku":
			if a.SKU != b.SKU {
				if desc {
					return a.SKU > b.SKU
				}
				return a.SKU < b.SKU
			}
		case "price":
			if a.Price != b.Price {
				if desc {
					return a.Price > b.Price
				}
				return a.Price < b.Price
			}
		case "createdAt":
			if !a.CreatedAt.Equal(b.CreatedAt) {
				if desc {
					return a.CreatedAt.After(b.CreatedAt)
				}
				return a.CreatedAt.Before(b.CreatedAt)
			}
		default:
			if a.Name != b.Name {
				if desc {
					return a.Name > b.Name
				}
				return a.Name < b.Name
			}
		}

		return a.ID.String() < b.ID.String()
	})

	total := int64(len(matched))

	start := filter.Offset
	if start > len(matched) {
		start = len(matched)
	}
	end := start + filter.Limit
	if end > len(matched) {
		end = len(matched)
	}

	return matched[start:end], total, nil
}

func TestProductService_Create_MissingName(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	_, err := service.Create(context.Background(), uuid.New(), "   ", "", "SKU-1", 1000, "", nil)

	if !errors.Is(err, ErrProductNameRequired) {
		t.Fatalf("expected ErrProductNameRequired, got %v", err)
	}
}

func TestProductService_Create_MissingSKU(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	_, err := service.Create(context.Background(), uuid.New(), "Widget", "", "   ", 1000, "", nil)

	if !errors.Is(err, ErrProductSKURequired) {
		t.Fatalf("expected ErrProductSKURequired, got %v", err)
	}
}

func TestProductService_Create_NegativePrice(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	_, err := service.Create(context.Background(), uuid.New(), "Widget", "", "SKU-1", -1, "", nil)

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
		nil,
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

func TestProductService_Create_StoresVATRate(t *testing.T) {
	service := NewProductService(newFakeProductRepository())
	vatRate := 20.0

	p, err := service.Create(context.Background(), uuid.New(), "Widget", "", "SKU-1", 1000, "", &vatRate)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if p.VATRate == nil || *p.VATRate != 20 {
		t.Errorf("expected VAT rate 20, got %v", p.VATRate)
	}
}

func TestProductService_Create_RejectsVATRateOutOfRange(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	for _, rate := range []float64{-0.01, 100.01} {
		_, err := service.Create(context.Background(), uuid.New(), "Widget", "", "SKU-1", 1000, "", &rate)
		if !errors.Is(err, ErrProductVATRateInvalid) {
			t.Errorf("VAT rate %v: expected ErrProductVATRateInvalid, got %v", rate, err)
		}
	}
}

func TestProductService_Update(t *testing.T) {
	repository := newFakeProductRepository()
	service := NewProductService(repository)
	organisationID := uuid.New()
	vatRate := 5.0

	created, err := service.Create(context.Background(), organisationID, "Widget", "Old", "SKU-1", 1000, "Old category", nil)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	updated, err := service.Update(context.Background(), organisationID, created.ID, "  Gadget  ", "", "  SKU-2  ", 2500, "Tools", &vatRate, nil)
	if err != nil {
		t.Fatalf("update product: %v", err)
	}

	if updated.ID != created.ID {
		t.Errorf("expected the ID to stay %v, got %v", created.ID, updated.ID)
	}
	if updated.Name != "Gadget" || updated.SKU != "SKU-2" || updated.Price != 2500 {
		t.Errorf("unexpected updated product: %+v", updated)
	}
	if updated.Description != nil {
		t.Errorf("expected a blank description to be cleared, got %q", *updated.Description)
	}
	if updated.Category == nil || *updated.Category != "Tools" {
		t.Errorf("expected category Tools, got %v", updated.Category)
	}
	if updated.VATRate == nil || *updated.VATRate != 5 {
		t.Errorf("expected VAT rate 5, got %v", updated.VATRate)
	}
	if !updated.IsActive {
		t.Error("expected an omitted isActive to leave the product active")
	}

	inactive := false
	updated, err = service.Update(context.Background(), organisationID, created.ID, "Gadget", "", "SKU-2", 2500, "", nil, &inactive)
	if err != nil {
		t.Fatalf("deactivate product: %v", err)
	}
	if updated.IsActive {
		t.Error("expected isActive false to deactivate the product")
	}
	if updated.VATRate != nil {
		t.Errorf("expected an omitted VAT rate to be cleared, got %v", *updated.VATRate)
	}
}

func TestProductService_Update_ValidatesLikeCreate(t *testing.T) {
	service := NewProductService(newFakeProductRepository())
	organisationID := uuid.New()
	badRate := 150.0

	created, err := service.Create(context.Background(), organisationID, "Widget", "", "SKU-1", 1000, "", nil)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	cases := []struct {
		name    string
		pName   string
		sku     string
		price   int64
		vatRate *float64
		want    error
	}{
		{"blank name", "  ", "SKU-1", 0, nil, ErrProductNameRequired},
		{"blank sku", "Widget", "  ", 0, nil, ErrProductSKURequired},
		{"negative price", "Widget", "SKU-1", -1, nil, ErrProductPriceNegative},
		{"VAT rate over 100", "Widget", "SKU-1", 0, &badRate, ErrProductVATRateInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Update(context.Background(), organisationID, created.ID, tc.pName, "", tc.sku, tc.price, "", tc.vatRate, nil)
			if !errors.Is(err, tc.want) {
				t.Errorf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestProductService_Update_OtherOrganisationIsNotFound(t *testing.T) {
	service := NewProductService(newFakeProductRepository())

	created, err := service.Create(context.Background(), uuid.New(), "Widget", "", "SKU-1", 1000, "", nil)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	_, err = service.Update(context.Background(), uuid.New(), created.ID, "Widget", "", "SKU-1", 1000, "", nil, nil)
	if !errors.Is(err, ErrProductNotFound) {
		t.Errorf("expected ErrProductNotFound, got %v", err)
	}
}
