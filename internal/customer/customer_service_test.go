package customer

import (
	"context"
	"errors"
	"sort"
	"strings"
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

// List is a simple in-memory re-implementation of the same filter/sort/
// paginate contract PostgresCustomerRepository.List implements in SQL —
// good enough for handler-level tests to exercise query parsing and
// error mapping; the real SQL predicates (ILIKE, ORDER BY, tenant
// scoping) are proven separately by customer_repository_postgres_test.go
// against real Postgres.
func (f *fakeCustomerRepository) List(ctx context.Context, organisationID uuid.UUID, filter ListFilter) ([]*Customer, int64, error) {
	var matched []*Customer

	for i := range f.customers {
		c := f.customers[i]
		if c.OrganisationID != organisationID {
			continue
		}
		if filter.Status != "" && c.Status != filter.Status {
			continue
		}
		if filter.Search != "" {
			needle := strings.ToLower(filter.Search)
			name := strings.ToLower(c.Name)
			company := ""
			if c.CompanyName != nil {
				company = strings.ToLower(*c.CompanyName)
			}
			email := ""
			if c.Email != nil {
				email = strings.ToLower(*c.Email)
			}
			if !strings.Contains(name, needle) && !strings.Contains(company, needle) && !strings.Contains(email, needle) {
				continue
			}
		}
		cCopy := c
		matched = append(matched, &cCopy)
	}

	desc := strings.EqualFold(filter.Order, "desc")

	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]

		switch filter.Sort {
		case "companyName":
			ca, cb := "", ""
			if a.CompanyName != nil {
				ca = *a.CompanyName
			}
			if b.CompanyName != nil {
				cb = *b.CompanyName
			}
			if ca != cb {
				if desc {
					return ca > cb
				}
				return ca < cb
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

		// Tie on the primary field: id ascending is the stable secondary
		// key, matching PostgresCustomerRepository.List's "ORDER BY ...,
		// id ASC" regardless of the primary field's own direction.
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
