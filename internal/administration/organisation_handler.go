package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
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
	var organisation Organisation

	if err := json.NewDecoder(r.Body).Decode(&organisation); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.service.Create(r.Context(), &organisation); err != nil {
		http.Error(w, "failed to create organisation", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(organisation); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// GetByID handles GET /organisations/{id}.
func (h *OrganisationHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idString := r.PathValue("id")

	id, err := uuid.Parse(idString)
	if err != nil {
		http.Error(w, "invalid organisation ID", http.StatusBadRequest)
		return
	}

	organisation, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrOrganisationNotFound) {
			http.Error(w, "organisation not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get organisation", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(organisation); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
