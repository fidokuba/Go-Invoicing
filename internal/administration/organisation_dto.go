package admin

import "time"

// CreateOrganisationRequest is the shape a client may POST to create an
// organisation. It deliberately exposes only what a caller is allowed to
// set — not ID, CreatedAt, DeletedAt, or anything else the server owns.
type CreateOrganisationRequest struct {
	Name string `json:"name"`
}

// OrganisationResponse is the shape returned to clients. It's a separate
// type from Organisation so the API's wire format can stay stable even if
// the internal/database model changes, and so internal-only fields (like
// DeletedAt) never leak out.
type OrganisationResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     *string `json:"email,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Website   *string `json:"website,omitempty"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// toOrganisationResponse maps the internal domain model onto the API's
// response shape.
func toOrganisationResponse(organisation *Organisation) OrganisationResponse {
	return OrganisationResponse{
		ID:        organisation.ID.String(),
		Name:      organisation.Name,
		Email:     organisation.Email,
		Phone:     organisation.Phone,
		Website:   organisation.Website,
		CreatedAt: organisation.CreatedAt.Format(time.RFC3339),
		UpdatedAt: organisation.UpdatedAt.Format(time.RFC3339),
	}
}
