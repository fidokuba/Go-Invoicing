package customer

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrCustomerNameRequired is returned when Create is called with an empty
// (or whitespace-only) name.
var ErrCustomerNameRequired = errors.New("customer name is required")

// CustomerService sits between the HTTP layer and the repository. It
// depends on the CustomerRepository interface, not on any concrete
// implementation.
type CustomerService struct {
	repository CustomerRepository
}

func NewCustomerService(
	repository CustomerRepository,
) *CustomerService {
	return &CustomerService{
		repository: repository,
	}
}

// Create validates the requested name, generates the customer's ID, and
// persists it under the given organisation. Optional fields left blank are
// stored as NULL rather than empty strings.
func (s *CustomerService) Create(
	ctx context.Context,
	organisationID uuid.UUID,
	name string,
	email string,
	phone string,
	companyName string,
	taxID string,
) (*Customer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrCustomerNameRequired
	}

	c := &Customer{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           name,
		Email:          nilIfEmpty(email),
		Phone:          nilIfEmpty(phone),
		CompanyName:    nilIfEmpty(companyName),
		TaxID:          nilIfEmpty(taxID),
		Status:         "active",
	}

	if err := s.repository.Create(ctx, c); err != nil {
		return nil, err
	}

	return c, nil
}

// GetByID delegates straight to the repository; the organisation scoping
// happens there.
func (s *CustomerService) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
) (*Customer, error) {
	return s.repository.GetByID(ctx, organisationID, customerID)
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
