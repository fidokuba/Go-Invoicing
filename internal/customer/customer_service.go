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

// ErrCustomerStatusInvalid is returned when List's ?status= filter isn't
// one of the CustomerStatus* constants — this is business-domain
// validation (which values the customers.status column may actually
// mean), so it lives here rather than in the handler.
var ErrCustomerStatusInvalid = errors.New("customer status is not valid")

// Billing address field-required errors (Milestone 7 Part 1). State is
// deliberately not in this list — not every country uses one, and the
// schema/DTO already treat it as optional.
var (
	ErrBillingAddressStreetRequired     = errors.New("billing address street is required")
	ErrBillingAddressCityRequired       = errors.New("billing address city is required")
	ErrBillingAddressPostalCodeRequired = errors.New("billing address postal code is required")
	ErrBillingAddressCountryRequired    = errors.New("billing address country is required")
)

// CustomerService sits between the HTTP layer and the repository. It also
// depends on AddressRepository for a customer's billing address
// (Milestone 7 Part 1) — treated as part of the customer aggregate rather
// than a separate service, the same way InvoiceService owns payment
// operations directly rather than delegating to a PaymentService.
type CustomerService struct {
	repository        CustomerRepository
	addressRepository AddressRepository
}

func NewCustomerService(
	repository CustomerRepository,
	addressRepository AddressRepository,
) *CustomerService {
	return &CustomerService{
		repository:        repository,
		addressRepository: addressRepository,
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
		Status:         CustomerStatusActive,
	}

	if err := s.repository.Create(ctx, c); err != nil {
		return nil, err
	}

	return c, nil
}

// List validates filter.Status (if supplied) against the known
// CustomerStatus* values and delegates to the repository, which enforces
// tenant scoping and the Search/Sort/Order/Limit/Offset predicates in
// SQL. Sort/Order themselves are not re-validated here — by the time a
// handler calls List, httpx.ParseSortOrder has already confirmed Sort is
// one of the repository's known public field names.
func (s *CustomerService) List(
	ctx context.Context,
	organisationID uuid.UUID,
	filter ListFilter,
) ([]*Customer, int64, error) {
	if filter.Status != "" {
		switch filter.Status {
		case CustomerStatusActive, CustomerStatusInactive, CustomerStatusArchived:
		default:
			return nil, 0, ErrCustomerStatusInvalid
		}
	}

	return s.repository.List(ctx, organisationID, filter)
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

// GetBillingAddress delegates straight to the repository; organisation
// scoping happens there, joined through the customer.
func (s *CustomerService) GetBillingAddress(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
) (*Address, error) {
	return s.addressRepository.GetBillingAddressByCustomerID(ctx, organisationID, customerID)
}

// UpsertBillingAddress validates the request and creates or replaces
// customerID's billing address — see AddressRepository.UpsertBillingAddress
// for how the "create if absent, replace if present" behaviour is a
// single atomic operation rather than a read-then-write this service
// performs itself.
//
// Street, City, PostalCode and Country must all be non-blank after
// trimming: an address record that's missing any of these isn't useful on
// an invoice. State is trimmed but may be blank — not every country uses
// one.
func (s *CustomerService) UpsertBillingAddress(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
	request UpsertBillingAddressRequest,
) (*Address, error) {
	street := strings.TrimSpace(request.Street)
	if street == "" {
		return nil, ErrBillingAddressStreetRequired
	}

	city := strings.TrimSpace(request.City)
	if city == "" {
		return nil, ErrBillingAddressCityRequired
	}

	postalCode := strings.TrimSpace(request.PostalCode)
	if postalCode == "" {
		return nil, ErrBillingAddressPostalCodeRequired
	}

	country := strings.TrimSpace(request.Country)
	if country == "" {
		return nil, ErrBillingAddressCountryRequired
	}

	address := &Address{
		ID:         uuid.New(),
		CustomerID: customerID,
		Type:       AddressTypeBilling,
		Street:     street,
		City:       city,
		State:      strings.TrimSpace(request.State),
		PostalCode: postalCode,
		Country:    country,
	}

	return s.addressRepository.UpsertBillingAddress(ctx, organisationID, customerID, address)
}
