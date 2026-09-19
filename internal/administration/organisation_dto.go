package admin

import "time"

// OrganisationResponse is the shape returned to clients. It's a separate
// type from Organisation so the API's wire format can stay stable even if
// the internal/database model changes, and so internal-only fields (like
// DeletedAt) never leak out. Logo is deliberately not exposed —
// Milestone 7 Part 1 doesn't touch logo upload/storage, and the column is
// never set by anything today.
type OrganisationResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Email      *string `json:"email,omitempty"`
	Phone      *string `json:"phone,omitempty"`
	Website    *string `json:"website,omitempty"`
	Address    *string `json:"address,omitempty"`
	City       *string `json:"city,omitempty"`
	State      *string `json:"state,omitempty"`
	PostalCode *string `json:"postalCode,omitempty"`
	Country    *string `json:"country,omitempty"`
	TaxID      *string `json:"taxId,omitempty"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

// toOrganisationResponse maps the internal domain model onto the API's
// response shape.
func toOrganisationResponse(organisation *Organisation) OrganisationResponse {
	return OrganisationResponse{
		ID:         organisation.ID.String(),
		Name:       organisation.Name,
		Email:      organisation.Email,
		Phone:      organisation.Phone,
		Website:    organisation.Website,
		Address:    organisation.Address,
		City:       organisation.City,
		State:      organisation.State,
		PostalCode: organisation.PostalCode,
		Country:    organisation.Country,
		TaxID:      organisation.TaxID,
		CreatedAt:  organisation.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  organisation.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// UpdateOrganisationRequest is the shape a client PATCHes to
// /organisation. Every field is a pointer so the request can distinguish
// "omitted — leave unchanged" (nil) from "explicitly supplied" (non-nil,
// possibly an empty string, which OrganisationService.Update treats as
// "clear this optional field" for everything except Name, which may never
// become blank). This is genuine partial-update semantics, not a
// generic/reflection-based patch — each field is applied explicitly in
// OrganisationService.Update.
//
// Logo is deliberately not present here — branding/logo upload is a
// future feature (Milestone 7 Part 1 explicitly excludes it), and the
// column is left untouched by this endpoint entirely.
type UpdateOrganisationRequest struct {
	Name       *string `json:"name"`
	Email      *string `json:"email"`
	Phone      *string `json:"phone"`
	Website    *string `json:"website"`
	Address    *string `json:"address"`
	City       *string `json:"city"`
	State      *string `json:"state"`
	PostalCode *string `json:"postalCode"`
	Country    *string `json:"country"`
	TaxID      *string `json:"taxId"`
}
