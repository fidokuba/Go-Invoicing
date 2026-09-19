package customer

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// WithTx ignores its tx argument and returns the same fake — it has no
// real transactional semantics of its own, matching every other fake
// repository's WithTx in this project.
func (f *fakeCustomerRepository) WithTx(tx pgx.Tx) CustomerRepository {
	return f
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

// fakeAddressRepository is an in-memory AddressRepository used to test
// CustomerService's billing-address methods without touching PostgreSQL.
// The real repository derives "does this customer belong to this
// organisation" from a live join against the customers table; this fake
// has no such table to join against, so tests register that relationship
// explicitly via registerCustomer first — the fake then enforces it
// exactly the way the real repository's JOIN would.
type fakeAddressRepository struct {
	mu                    sync.Mutex
	customerOrganisations map[uuid.UUID]uuid.UUID
	billingAddresses      map[uuid.UUID]Address
}

func newFakeAddressRepository() *fakeAddressRepository {
	return &fakeAddressRepository{
		customerOrganisations: make(map[uuid.UUID]uuid.UUID),
		billingAddresses:      make(map[uuid.UUID]Address),
	}
}

func (f *fakeAddressRepository) registerCustomer(customerID, organisationID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.customerOrganisations[customerID] = organisationID
}

// WithTx ignores its tx argument and returns the same fake — it has no
// real transactional semantics of its own, matching every other fake
// repository's WithTx in this project.
func (f *fakeAddressRepository) WithTx(tx pgx.Tx) AddressRepository {
	return f
}

func (f *fakeAddressRepository) GetBillingAddressByCustomerID(ctx context.Context, organisationID, customerID uuid.UUID) (*Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.customerOrganisations[customerID] != organisationID {
		return nil, ErrBillingAddressNotFound
	}

	a, ok := f.billingAddresses[customerID]
	if !ok {
		return nil, ErrBillingAddressNotFound
	}

	result := a
	return &result, nil
}

// UpsertBillingAddress preserves the existing row's ID across an update —
// mirroring the real repository's ON CONFLICT ... DO UPDATE, which never
// creates a second row for the same customer.
func (f *fakeAddressRepository) UpsertBillingAddress(ctx context.Context, organisationID, customerID uuid.UUID, address *Address) (*Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.customerOrganisations[customerID] != organisationID {
		return nil, ErrBillingAddressNotFound
	}

	stored := *address
	if existing, ok := f.billingAddresses[customerID]; ok {
		stored.ID = existing.ID
	}
	f.billingAddresses[customerID] = stored

	result := stored
	return &result, nil
}

func TestCustomerService_Create_MissingName(t *testing.T) {
	service := NewCustomerService(newFakeCustomerRepository(), newFakeAddressRepository())

	_, err := service.Create(context.Background(), uuid.New(), "   ", "", "", "", "")

	if !errors.Is(err, ErrCustomerNameRequired) {
		t.Fatalf("expected ErrCustomerNameRequired, got %v", err)
	}
}

func TestCustomerService_Create_Success(t *testing.T) {
	organisationID := uuid.New()
	service := NewCustomerService(newFakeCustomerRepository(), newFakeAddressRepository())

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
