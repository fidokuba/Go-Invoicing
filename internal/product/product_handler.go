package product

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/httpx"
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
	if !httpx.DecodeJSON(w, r, &request) {
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
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		if errors.Is(err, ErrProductSKUAlreadyExists) {
			httpx.WriteError(w, http.StatusConflict, "sku_already_exists", err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to create product")
		return
	}

	response := toProductResponse(p)

	httpx.WriteJSON(w, http.StatusCreated, response)
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
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid product ID")
		return
	}

	p, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "product_not_found", "product not found")
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get product")
		return
	}

	response := toProductResponse(p)

	httpx.WriteJSON(w, http.StatusOK, response)
}
