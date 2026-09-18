package product

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

// ProductHandler owns the HTTP-specific concerns for products: decoding
// requests, calling the service, translating errors into status codes, and
// encoding responses. It holds no SQL and no business rules.
//
// Both routes are protected (Milestone 4 Part 4): organisation identity
// comes exclusively from admin.RequireAuthenticatedUser, never from a
// client-supplied organisationId — see that function's doc comment for
// the fail-closed behaviour when no authenticated identity is present.
type ProductHandler struct {
	service *ProductService
}

func NewProductHandler(
	service *ProductService,
) *ProductHandler {
	return &ProductHandler{
		service: service,
	}
}

// Create handles POST /products.
func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request CreateProductRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	p, err := h.service.Create(
		r.Context(),
		identity.OrganisationID,
		request.Name,
		request.Description,
		request.SKU,
		request.Price,
		request.Category,
	)
	if err != nil {
		if errors.Is(err, ErrProductNameRequired) ||
			errors.Is(err, ErrProductSKURequired) ||
			errors.Is(err, ErrProductPriceNegative) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if errors.Is(err, ErrProductSKUAlreadyExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		http.Error(w, "failed to create product", http.StatusInternalServerError)
		return
	}

	response := toProductResponse(p)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// GetByID handles GET /products/{id}.
func (h *ProductHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	idString := r.PathValue("id")

	id, err := uuid.Parse(idString)
	if err != nil {
		http.Error(w, "invalid product ID", http.StatusBadRequest)
		return
	}

	p, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			http.Error(w, "product not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get product", http.StatusInternalServerError)
		return
	}

	response := toProductResponse(p)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
