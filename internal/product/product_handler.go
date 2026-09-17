package product

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// ProductHandler owns the HTTP-specific concerns for products: decoding
// requests, calling the service, translating errors into status codes, and
// encoding responses. It holds no SQL and no business rules.
//
// There is no authentication/organisation-identity middleware yet, so the
// organisation scope is read explicitly from an "organisationId" query
// parameter on every request, matching the Customer handler's convention.
// This is expected to be replaced once request-scoped organisation
// identity exists.
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

// Create handles POST /products?organisationId={organisationId}.
func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	organisationID, err := uuid.Parse(r.URL.Query().Get("organisationId"))
	if err != nil {
		http.Error(w, "invalid or missing organisationId", http.StatusBadRequest)
		return
	}

	var request CreateProductRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	p, err := h.service.Create(
		r.Context(),
		organisationID,
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

// GetByID handles GET /products/{id}?organisationId={organisationId}.
func (h *ProductHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	organisationID, err := uuid.Parse(r.URL.Query().Get("organisationId"))
	if err != nil {
		http.Error(w, "invalid or missing organisationId", http.StatusBadRequest)
		return
	}

	idString := r.PathValue("id")

	id, err := uuid.Parse(idString)
	if err != nil {
		http.Error(w, "invalid product ID", http.StatusBadRequest)
		return
	}

	p, err := h.service.GetByID(r.Context(), organisationID, id)
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
