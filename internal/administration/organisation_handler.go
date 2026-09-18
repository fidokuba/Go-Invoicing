package admin

import (
	"encoding/json"
	"errors"
	"net/http"
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

// Create handles POST /organisations.
func (h *OrganisationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateOrganisationRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	organisation, err := h.service.Create(r.Context(), request.Name)
	if err != nil {
		if errors.Is(err, ErrOrganisationNameRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		http.Error(w, "failed to create organisation", http.StatusInternalServerError)
		return
	}

	response := toOrganisationResponse(organisation)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
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
			http.Error(w, "organisation not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get organisation", http.StatusInternalServerError)
		return
	}

	response := toOrganisationResponse(organisation)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
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

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	organisation, err := h.service.Update(r.Context(), identity.OrganisationID, request)
	if err != nil {
		if errors.Is(err, ErrOrganisationNotFound) {
			http.Error(w, "organisation not found", http.StatusNotFound)
			return
		}

		if errors.Is(err, ErrOrganisationNameRequired) || errors.Is(err, ErrOrganisationEmailInvalid) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		http.Error(w, "failed to update organisation", http.StatusInternalServerError)
		return
	}

	response := toOrganisationResponse(organisation)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
