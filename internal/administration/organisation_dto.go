package admin

import "time"

// OrganisationResponse is the shape returned to clients. It's a separate
// type from Organisation so the API's wire format can stay stable even if
// the internal/database model changes, and so internal-only fields (like
// DeletedAt) never leak out. LogoID identifies the current logo (fetch
// the image itself from GET /organisation/logo); a new ID on every upload
// makes it a natural cache key for clients.
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
	LogoID     *string `json:"logoId,omitempty"`
	// VATRegistered is always present. TaxID is still returned while it's
	// false (so the settings form can restore it when re-ticked); clients
	// are expected not to display it in that state.
	VATRegistered bool   `json:"vatRegistered"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// toOrganisationResponse maps the internal domain model onto the API's
// response shape.
func toOrganisationResponse(organisation *Organisation) OrganisationResponse {
	return OrganisationResponse{
		ID:            organisation.ID.String(),
		Name:          organisation.Name,
		Email:         organisation.Email,
		Phone:         organisation.Phone,
		Website:       organisation.Website,
		Address:       organisation.Address,
		City:          organisation.City,
		State:         organisation.State,
		PostalCode:    organisation.PostalCode,
		Country:       organisation.Country,
		TaxID:         organisation.TaxID,
		LogoID:        optionalUUIDString(organisation.LogoID),
		VATRegistered: organisation.VATRegistered,
		CreatedAt:     organisation.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     organisation.UpdatedAt.UTC().Format(time.RFC3339),
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
// The logo is not part of this request — it has its own endpoints
// (PUT/DELETE /organisation/logo), and this one never touches it.
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

	// VATRegistered, like every other field, is left unchanged when
	// omitted. Setting it to true requires the organisation to end up
	// with a non-blank TaxID (its VAT registration number).
	VATRegistered *bool `json:"vatRegistered"`
}
