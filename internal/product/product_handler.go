package product

import (
	"errors"
	"net/http"
	"strconv"

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

// ProductListSortFields is the public sort-field allow-list for GET
// /products, validated by httpx.ParseSortOrder before List ever runs —
// see productSortColumns in product_repository_postgres.go for how each
// of these maps onto an actual SQL column. Exported (Milestone 8 Part 5)
// so the OpenAPI route/query contract tests can compare the maintained
// spec's documented sort enum against this handler's actual allow-list
// without a second, hand-duplicated copy of it living in a test file.
var ProductListSortFields = []string{"name", "sku", "price", "createdAt"}

// ProductListQueryParams is the complete set of query parameters GET
// /products recognises — anything else is rejected with 400 (see
// httpx.RejectUnknownQueryParams). Exported for the same contract-testing
// reason as ProductListSortFields above.
var ProductListQueryParams = []string{"limit", "offset", "search", "isActive", "sort", "order"}

// ProductListDefaultSort and ProductListDefaultOrder are GET /products'
// defaults when "sort"/"order" are omitted — exported for the same
// contract-testing reason as ProductListSortFields above.
const (
	ProductListDefaultSort  = "name"
	ProductListDefaultOrder = "asc"
)

// List handles GET /products (Milestone 8 Part 3) — available to every
// authenticated role, same policy as every other product route. Default
// sort is name ascending, for the same "a catalogue is browsed
// alphabetically by default" reasoning as GET /customers.
func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	if !httpx.RejectUnknownQueryParams(w, r, ProductListQueryParams...) {
		return
	}

	limit, offset, ok := httpx.ParseLimitOffset(w, r)
	if !ok {
		return
	}

	sort, order, ok := httpx.ParseSortOrder(w, r, ProductListSortFields, ProductListDefaultSort, ProductListDefaultOrder)
	if !ok {
		return
	}

	query := r.URL.Query()
	search, _ := httpx.OptionalQueryParam(query, "search")

	var isActive *bool
	if raw, present := httpx.OptionalQueryParam(query, "isActive"); present {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, "isActive must be true or false")
			return
		}
		isActive = &parsed
	}

	filter := ListFilter{
		Search:   search,
		IsActive: isActive,
		Sort:     sort,
		Order:    order,
		Limit:    limit,
		Offset:   offset,
	}

	products, total, err := h.service.List(r.Context(), identity.OrganisationID, filter)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to list products")
		return
	}

	items := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		items = append(items, toProductResponse(p))
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.NewListResponse(items, limit, offset, total))
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
