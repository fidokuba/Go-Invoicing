package customer

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

// CustomerHandler owns the HTTP-specific concerns for customers: decoding
// requests, calling the service, translating errors into status codes, and
// encoding responses. It holds no SQL and no business rules.
//
// Both routes are protected (Milestone 4 Part 4): organisation identity
// comes exclusively from admin.RequireAuthenticatedUser, never from a
// client-supplied organisationId — see that function's doc comment for
// the fail-closed behaviour when no authenticated identity is present.
type CustomerHandler struct {
	service *CustomerService
}

func NewCustomerHandler(
	service *CustomerService,
) *CustomerHandler {
	return &CustomerHandler{
		service: service,
	}
}

// Create handles POST /customers.
func (h *CustomerHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request CreateCustomerRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	c, err := h.service.Create(
		r.Context(),
		identity.OrganisationID,
		request.Name,
		request.Email,
		request.Phone,
		request.CompanyName,
		request.TaxID,
	)
	if err != nil {
		if errors.Is(err, ErrCustomerNameRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		http.Error(w, "failed to create customer", http.StatusInternalServerError)
		return
	}

	response := toCustomerResponse(c)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// GetByID handles GET /customers/{id}.
func (h *CustomerHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	idString := r.PathValue("id")

	id, err := uuid.Parse(idString)
	if err != nil {
		http.Error(w, "invalid customer ID", http.StatusBadRequest)
		return
	}

	c, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrCustomerNotFound) {
			http.Error(w, "customer not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get customer", http.StatusInternalServerError)
		return
	}

	response := toCustomerResponse(c)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
