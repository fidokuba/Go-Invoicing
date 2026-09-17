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
		CreatedAt:      c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      c.UpdatedAt.Format(time.RFC3339),
	}
}
