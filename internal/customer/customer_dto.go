package customer

import "time"

// CreateCustomerRequest is the shape a client may POST to create a
// customer. It deliberately exposes only what a caller is allowed to set —
// not ID, OrganisationID, Status, CreatedAt, DeletedAt, or anything else
// the server owns.
type CreateCustomerRequest struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CompanyName string `json:"companyName"`
	TaxID       string `json:"taxId"`
}

// CustomerResponse is the shape returned to clients. It exposes the public
// customer fields but never DeletedAt.
type CustomerResponse struct {
	ID             string  `json:"id"`
	OrganisationID string  `json:"organisationId"`
	Name           string  `json:"name"`
	Email          *string `json:"email,omitempty"`
	Phone          *string `json:"phone,omitempty"`
	CompanyName    *string `json:"companyName,omitempty"`
	TaxID          *string `json:"taxId,omitempty"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
}

// toCustomerResponse maps the internal domain model onto the API's response
// shape.
func toCustomerResponse(c *Customer) CustomerResponse {
	return CustomerResponse{
		ID:             c.ID.String(),
		OrganisationID: c.OrganisationID.String(),
		Name:           c.Name,
		Email:          c.Email,
		Phone:          c.Phone,
		CompanyName:    c.CompanyName,
		TaxID:          c.TaxID,
		Status:         c.Status,
		CreatedAt:      c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// UpsertBillingAddressRequest is the shape a client PUTs to
// /customers/{id}/billing-address. This is a whole-resource replace, not
// a partial patch (see PUT semantics on the handler), so every field is a
// plain string — there is no need to distinguish "omitted" from
// "explicitly empty" the way UpdateOrganisationRequest does. Street,
// City, PostalCode and Country are required by CustomerService
// .UpsertBillingAddress; State may be left blank.
type UpsertBillingAddressRequest struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
}

// AddressResponse is the shape returned for a customer's billing address.
type AddressResponse struct {
	ID         string `json:"id"`
	CustomerID string `json:"customerId"`
	Type       string `json:"type"`
	Street     string `json:"street"`
	City       string `json:"city"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// toAddressResponse maps the internal domain model onto the API's response
// shape. IsDefault is deliberately not exposed — see Address's own doc
// comment for why nothing reads it yet.
func toAddressResponse(a *Address) AddressResponse {
	return AddressResponse{
		ID:         a.ID.String(),
		CustomerID: a.CustomerID.String(),
		Type:       a.Type,
		Street:     a.Street,
		City:       a.City,
		State:      a.State,
		PostalCode: a.PostalCode,
		Country:    a.Country,
		CreatedAt:  a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
