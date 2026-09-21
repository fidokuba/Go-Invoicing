package customer

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/httpx"
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
	if !httpx.DecodeJSON(w, r, &request) {
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
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to create customer")
		return
	}

	response := toCustomerResponse(c)

	httpx.WriteJSON(w, http.StatusCreated, response)
}

// CustomerListSortFields is the public sort-field allow-list for GET
// /customers, validated by httpx.ParseSortOrder before List ever runs —
// see customerSortColumns in customer_repository_postgres.go for how
// each of these maps onto an actual SQL column. Exported (Milestone 8
// Part 5) so the OpenAPI route/query contract tests can compare the
// maintained spec's documented sort enum against this handler's actual
// allow-list without a second, hand-duplicated copy of it living in a
// test file.
var CustomerListSortFields = []string{"name", "companyName", "createdAt"}

// CustomerListQueryParams is the complete set of query parameters GET
// /customers recognises — anything else is rejected with 400 (see
// httpx.RejectUnknownQueryParams). Exported for the same
// contract-testing reason as CustomerListSortFields above.
var CustomerListQueryParams = []string{"limit", "offset", "search", "status", "sort", "order"}

// CustomerListDefaultSort and CustomerListDefaultOrder are GET
// /customers' defaults when "sort"/"order" are omitted — exported for
// the same contract-testing reason as CustomerListSortFields above.
const (
	CustomerListDefaultSort  = "name"
	CustomerListDefaultOrder = "asc"
)

// List handles GET /customers (Milestone 8 Part 3) — available to every
// authenticated role, same policy as every other customer route.
// Default sort is name ascending: a customer list is most usefully
// browsed alphabetically by default, unlike an invoice dashboard (which
// defaults to newest-first).
func (h *CustomerHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	if !httpx.RejectUnknownQueryParams(w, r, CustomerListQueryParams...) {
		return
	}

	limit, offset, ok := httpx.ParseLimitOffset(w, r)
	if !ok {
		return
	}

	sort, order, ok := httpx.ParseSortOrder(w, r, CustomerListSortFields, CustomerListDefaultSort, CustomerListDefaultOrder)
	if !ok {
		return
	}

	query := r.URL.Query()
	search, _ := httpx.OptionalQueryParam(query, "search")
	status, _ := httpx.OptionalQueryParam(query, "status")

	filter := ListFilter{
		Search: search,
		Status: status,
		Sort:   sort,
		Order:  order,
		Limit:  limit,
		Offset: offset,
	}

	customers, total, err := h.service.List(r.Context(), identity.OrganisationID, filter)
	if err != nil {
		if errors.Is(err, ErrCustomerStatusInvalid) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to list customers")
		return
	}

	items := make([]CustomerResponse, 0, len(customers))
	for _, c := range customers {
		items = append(items, toCustomerResponse(c))
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.NewListResponse(items, limit, offset, total))
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
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid customer ID")
		return
	}

	c, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrCustomerNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "customer_not_found", "customer not found")
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get customer")
		return
	}

	response := toCustomerResponse(c)

	httpx.WriteJSON(w, http.StatusOK, response)
}

// GetBillingAddress handles GET /customers/{id}/billing-address.
func (h *CustomerHandler) GetBillingAddress(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	customerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid customer ID")
		return
	}

	address, err := h.service.GetBillingAddress(r.Context(), identity.OrganisationID, customerID)
	if err != nil {
		if errors.Is(err, ErrBillingAddressNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "billing_address_not_found", "billing address not found")
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get billing address")
		return
	}

	response := toAddressResponse(address)

	httpx.WriteJSON(w, http.StatusOK, response)
}

// UpsertBillingAddress handles PUT /customers/{id}/billing-address —
// creates the customer's billing address if none exists yet, or replaces
// it in place if one already does (see CustomerService.UpsertBillingAddress).
func (h *CustomerHandler) UpsertBillingAddress(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	customerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid customer ID")
		return
	}

	var request UpsertBillingAddressRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	address, err := h.service.UpsertBillingAddress(r.Context(), identity.OrganisationID, customerID, request)
	if err != nil {
		if errors.Is(err, ErrBillingAddressNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "customer_not_found", "customer not found")
			return
		}

		if isBillingAddressValidationError(err) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to save billing address")
		return
	}

	response := toAddressResponse(address)

	httpx.WriteJSON(w, http.StatusOK, response)
}

// isBillingAddressValidationError reports whether err is one of
// CustomerService.UpsertBillingAddress's input-validation sentinels,
// which map to HTTP 400 — as opposed to ErrBillingAddressNotFound (404)
// or an unexpected failure (500).
func isBillingAddressValidationError(err error) bool {
	switch {
	case errors.Is(err, ErrBillingAddressStreetRequired),
		errors.Is(err, ErrBillingAddressCityRequired),
		errors.Is(err, ErrBillingAddressPostalCodeRequired),
		errors.Is(err, ErrBillingAddressCountryRequired):
		return true
	default:
		return false
	}
}
