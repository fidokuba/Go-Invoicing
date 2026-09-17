package invoice

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// InvoiceHandler owns the HTTP-specific concerns for invoices: decoding
// requests, calling the service, translating errors into status codes, and
// encoding responses. It holds no SQL and no business rules.
//
// There is no authentication/organisation-identity middleware yet, so the
// organisation scope is read explicitly from an "organisationId" query
// parameter on every request, matching the Customer/Product handlers'
// convention.
type InvoiceHandler struct {
	service *InvoiceService
}

func NewInvoiceHandler(
	service *InvoiceService,
) *InvoiceHandler {
	return &InvoiceHandler{
		service: service,
	}
}

// Create handles POST /invoices?organisationId={organisationId}.
func (h *InvoiceHandler) Create(w http.ResponseWriter, r *http.Request) {
	organisationID, err := uuid.Parse(r.URL.Query().Get("organisationId"))
	if err != nil {
		http.Error(w, "invalid or missing organisationId", http.StatusBadRequest)
		return
	}

	var request CreateInvoiceRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	inv, lines, err := h.service.Create(r.Context(), organisationID, request)
	if err != nil {
		if errors.Is(err, ErrInvoiceCustomerNotFound) || errors.Is(err, ErrInvoiceLineProductNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		if isInvoiceValidationError(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		http.Error(w, "failed to create invoice", http.StatusInternalServerError)
		return
	}

	response := toInvoiceResponse(inv, lines)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// GetByID handles GET /invoices/{id}?organisationId={organisationId}.
func (h *InvoiceHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	organisationID, err := uuid.Parse(r.URL.Query().Get("organisationId"))
	if err != nil {
		http.Error(w, "invalid or missing organisationId", http.StatusBadRequest)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid invoice ID", http.StatusBadRequest)
		return
	}

	inv, lines, err := h.service.GetByID(r.Context(), organisationID, id)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			http.Error(w, "invoice not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get invoice", http.StatusInternalServerError)
		return
	}

	response := toInvoiceResponse(inv, lines)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// isInvoiceValidationError reports whether err is one of
// InvoiceService.Create's input-validation sentinels, which map to HTTP
// 400 — as opposed to a not-found error (404) or an unexpected failure
// (500).
func isInvoiceValidationError(err error) bool {
	switch {
	case errors.Is(err, ErrInvoiceCustomerIDRequired),
		errors.Is(err, ErrInvoiceCustomerIDInvalid),
		errors.Is(err, ErrInvoiceNoLines),
		errors.Is(err, ErrInvoiceIssueDateRequired),
		errors.Is(err, ErrInvoiceIssueDateInvalid),
		errors.Is(err, ErrInvoiceDueDateRequired),
		errors.Is(err, ErrInvoiceDueDateInvalid),
		errors.Is(err, ErrInvoiceDueDateBeforeIssueDate),
		errors.Is(err, ErrInvoiceLineDescriptionRequired),
		errors.Is(err, ErrInvoiceLineQuantityInvalid),
		errors.Is(err, ErrInvoiceLineUnitPriceNegative),
		errors.Is(err, ErrInvoiceLineVATRateNegative),
		errors.Is(err, ErrInvoiceLineProductIDInvalid):
		return true
	default:
		return false
	}
}
