package admin

import (
	"errors"
	"net/http"

	"go-invoicing/internal/httpx"
)

// OrganisationHandler owns the HTTP-specific concerns for organisations:
// decoding requests, calling the service, translating errors into status
// codes, and encoding responses. It holds no SQL and no business rules.
type OrganisationHandler struct {
	service *OrganisationService
}

func NewOrganisationHandler(
	service *OrganisationService,
) *OrganisationHandler {
	return &OrganisationHandler{
		service: service,
	}
}

// GetCurrent handles GET /organisation — a self-resource endpoint with no
// client-supplied organisation ID at all (Milestone 4 Part 4). The
// organisation returned is always the authenticated caller's own: there
// is no path or query parameter for a tenant ID to select a different
// one, so there is no cross-tenant case to guard against here the way
// there is for every other GetByID in this codebase.
func (h *OrganisationHandler) GetCurrent(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	organisation, err := h.service.GetByID(r.Context(), identity.OrganisationID)
	if err != nil {
		if errors.Is(err, ErrOrganisationNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "organisation_not_found", "organisation not found")
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get organisation")
		return
	}

	response := toOrganisationResponse(organisation)

	httpx.WriteJSON(w, http.StatusOK, response)
}

// Update handles PATCH /organisation (Milestone 7 Part 1) — admin-only,
// enforced by the RequireRole middleware wrapping this route in app.go,
// not by anything in this handler. Organisation identity comes
// exclusively from the authenticated caller's own OrganisationID, exactly
// like GetCurrent: there is no {id} anywhere in this route for a client
// to supply, so there is no cross-tenant case to guard against here.
func (h *OrganisationHandler) Update(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request UpdateOrganisationRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	organisation, err := h.service.Update(r.Context(), identity.OrganisationID, request)
	if err != nil {
		if errors.Is(err, ErrOrganisationNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "organisation_not_found", "organisation not found")
			return
		}

		if errors.Is(err, ErrOrganisationNameRequired) || errors.Is(err, ErrOrganisationEmailInvalid) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to update organisation")
		return
	}

	response := toOrganisationResponse(organisation)

	httpx.WriteJSON(w, http.StatusOK, response)
}
