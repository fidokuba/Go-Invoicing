package invoice

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

// InvoiceHandler owns the HTTP-specific concerns for invoices: decoding
// requests, calling the service, translating errors into status codes, and
// encoding responses. It holds no SQL and no business rules.
//
// Every route is protected (Milestone 4 Part 4): organisation identity
// comes exclusively from admin.RequireAuthenticatedUser, never from a
// client-supplied organisationId — see that function's doc comment for
// the fail-closed behaviour when no authenticated identity is present.
// This matters most for payment creation/retrieval: the organisation ID
// passed to InvoiceService is what its existing organisation-scoped
// invoice lookup (and, for CreatePayment, its row lock) uses to establish
// that the invoice belongs to the caller before anything else happens.
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

// Create handles POST /invoices.
func (h *InvoiceHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request CreateInvoiceRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	inv, lines, err := h.service.Create(r.Context(), identity.OrganisationID, request)
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

	// A brand-new invoice cannot have any payments yet — no query needed
	// to know amountPaid is 0.
	response := toInvoiceResponse(inv, lines, 0)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// GetByID handles GET /invoices/{id}.
func (h *InvoiceHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid invoice ID", http.StatusBadRequest)
		return
	}

	inv, lines, amountPaid, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			http.Error(w, "invoice not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get invoice", http.StatusInternalServerError)
		return
	}

	response := toInvoiceResponse(inv, lines, amountPaid)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// CreatePayment handles POST /invoices/{id}/payments.
func (h *InvoiceHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	invoiceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid invoice ID", http.StatusBadRequest)
		return
	}

	var body CreatePaymentHTTPRequest

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	request, err := body.toCreatePaymentRequest()
	if err != nil {
		http.Error(w, "invalid payment date", http.StatusBadRequest)
		return
	}

	payment, _, err := h.service.CreatePayment(r.Context(), identity.OrganisationID, invoiceID, request)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			http.Error(w, "invoice not found", http.StatusNotFound)
			return
		}

		if errors.Is(err, ErrPaymentAmountInvalid) || errors.Is(err, ErrPaymentExceedsOutstanding) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		http.Error(w, "failed to create payment", http.StatusInternalServerError)
		return
	}

	response := toPaymentResponse(payment)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// GetPayments handles GET /invoices/{id}/payments.
func (h *InvoiceHandler) GetPayments(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	invoiceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid invoice ID", http.StatusBadRequest)
		return
	}

	payments, err := h.service.GetPayments(r.Context(), identity.OrganisationID, invoiceID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			http.Error(w, "invoice not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get payments", http.StatusInternalServerError)
		return
	}

	response := make([]PaymentResponse, 0, len(payments))
	for _, p := range payments {
		response = append(response, toPaymentResponse(p))
	}

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
