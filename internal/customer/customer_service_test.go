package customer

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// fakeCustomerRepository is an in-memory CustomerRepository used to test
// the service/handler without touching PostgreSQL. It mirrors the
// organisation-scoping behaviour of PostgresCustomerRepository.GetByID so
// tests exercise that behaviour too.
type fakeCustomerRepository struct {
	customers map[uuid.UUID]Customer
}

func newFakeCustomerRepository() *fakeCustomerRepository {
	return &fakeCustomerRepository{
		customers: make(map[uuid.UUID]Customer),
	}
}

func (f *fakeCustomerRepository) Create(ctx context.Context, c *Customer) error {
	f.customers[c.ID] = *c
	return nil
}

func (f *fakeCustomerRepository) GetByID(ctx context.Context, organisationID, customerID uuid.UUID) (*Customer, error) {
	c, ok := f.customers[customerID]
	if !ok || c.OrganisationID != organisationID {
		return nil, ErrCustomerNotFound
	}

	return &c, nil
}

func TestCustomerService_Create_MissingName(t *testing.T) {
	service := NewCustomerService(newFakeCustomerRepository())

	_, err := service.Create(context.Background(), uuid.New(), "   ", "", "", "", "")

	if !errors.Is(err, ErrCustomerNameRequired) {
		t.Fatalf("expected ErrCustomerNameRequired, got %v", err)
	}
}

func TestCustomerService_Create_Success(t *testing.T) {
	organisationID := uuid.New()
	service := NewCustomerService(newFakeCustomerRepository())

	c, err := service.Create(
		context.Background(),
		organisationID,
		"  Acme Ltd  ",
		"hello@acme.test",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	if c.ID == uuid.Nil {
		t.Error("expected a generated ID, got the nil UUID")
	}

	if c.OrganisationID != organisationID {
		t.Errorf("expected organisation ID %v, got %v", organisationID, c.OrganisationID)
	}

	if c.Name != "Acme Ltd" {
		t.Errorf("expected name to be trimmed to %q, got %q", "Acme Ltd", c.Name)
	}

	if c.Email == nil || *c.Email != "hello@acme.test" {
		t.Errorf("expected email %q, got %v", "hello@acme.test", c.Email)
	}

	if c.Phone != nil {
		t.Errorf("expected phone to be nil, got %v", *c.Phone)
	}

	if c.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", c.Status)
	}
}
