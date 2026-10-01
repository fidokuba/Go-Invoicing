package sampledata

import (
	"net/http"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/httpx"
)

// Handler owns the HTTP-specific concerns for the Create Test Data
// action: identity, calling Service, and encoding the response. It holds
// no business logic — see Service.Generate for all of that.
//
// Every input this action uses comes from inside the server itself (the
// fixed fake-data pools, the organisation's own VAT-registration flag),
// never from the request, so unlike every other handler in this project
// there is no client-supplied data to validate and therefore no 400/409
// this action can ever produce — a failure here is always a genuine,
// unexpected server-side problem, mapped to the generic 500 the same way
// ErrInvoicePDFDataUnavailable-style "lookup actually failed" cases are
// elsewhere.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Create handles POST /api/v1/test-data. Route registration (app.go)
// gates this to the admin role — RequireAuthenticatedUser below only
// recovers the caller's organisation identity, the same as every other
// handler in this project.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	summary, err := h.service.Generate(r.Context(), identity.OrganisationID)
	if err != nil {
		httpx.WriteInternalError(w, r, "sampledata.create", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, newCreateTestDataResponse(summary))
}
