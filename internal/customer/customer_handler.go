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
