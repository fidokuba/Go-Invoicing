package invoice

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

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
	service    *InvoiceService
	pdfService *InvoicePDFService
}

func NewInvoiceHandler(
	service *InvoiceService,
	pdfService *InvoicePDFService,
) *InvoiceHandler {
	return &InvoiceHandler{
		service:    service,
		pdfService: pdfService,
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
	response := toInvoiceResponse(inv, lines, 0, time.Now().UTC())

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

	response := toInvoiceResponse(inv, lines, amountPaid, time.Now().UTC())

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// Send handles POST /invoices/{id}/send — a lifecycle finalisation
// operation only (Milestone 5): it does not generate a PDF, send an
// email, or contact the customer. See InvoiceService.Send's doc comment
// for the transition and locking behaviour.
func (h *InvoiceHandler) Send(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid invoice ID", http.StatusBadRequest)
		return
	}

	if _, err := h.service.Send(r.Context(), identity.OrganisationID, id); err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			http.Error(w, "invoice not found", http.StatusNotFound)
			return
		}

		if errors.Is(err, ErrInvoiceAlreadySent) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		// Milestone 7 Part 2: the invoice exists and is a valid Draft, but
		// required business data for its immutable snapshot (seller name,
		// customer name, currency, or the organisation/customer/settings
		// records themselves) is missing or incomplete — a 409, the same
		// "exists but can't currently be finalised" category as an
		// already-Sent invoice, never a generic 500.
		if isSnapshotIncompleteError(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		http.Error(w, "failed to send invoice", http.StatusInternalServerError)
		return
	}

	// Send only returns the bare *Invoice; re-fetch through the existing
	// GetByID path to build the same full InvoiceResponse shape (lines +
	// amountPaid) every other invoice-returning endpoint already uses,
	// rather than duplicating that assembly here. A newly-sent invoice
	// cannot have any payments yet (CreatePayment already refuses a Draft
	// invoice), so this adds no surprising state, just the lines.
	inv, lines, amountPaid, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		http.Error(w, "failed to get invoice", http.StatusInternalServerError)
		return
	}

	response := toInvoiceResponse(inv, lines, amountPaid, time.Now().UTC())

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

		if errors.Is(err, ErrInvoiceCannotAcceptPayment) {
			http.Error(w, err.Error(), http.StatusConflict)
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

// GetPDF handles GET /invoices/{id}/pdf — synchronously generates and
// returns the invoice's PDF document (Milestone 7 Part 3). Tenant
// identity comes exclusively from the authenticated caller, exactly like
// every other invoice route; there is no organisationId query/body input
// anywhere in this handler for a client to influence.
func (h *InvoiceHandler) GetPDF(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	invoiceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid invoice ID", http.StatusBadRequest)
		return
	}

	pdfBytes, invoiceNumber, err := h.pdfService.Generate(r.Context(), identity.OrganisationID, invoiceID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			http.Error(w, "invoice not found", http.StatusNotFound)
			return
		}

		// Milestone 7 Part 3 section 6: an issued invoice missing a
		// required snapshot field, or a Draft invoice missing required
		// live seller/customer/currency data, both mean "this invoice
		// exists but cannot currently produce a historically reliable
		// document" — the same 409 category Send already uses for
		// business-data incompleteness, never a generic 500.
		if isPDFDataIncompleteError(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		// Renderer failures and any other unexpected repository error
		// are both genuine server-side problems — mapped to a single
		// generic message so no gopdf error, SQL detail, or filesystem
		// path is ever exposed to the client.
		http.Error(w, "failed to generate invoice pdf", http.StatusInternalServerError)
		return
	}

	filename := "invoice-" + SanitizeFilenameComponent(invoiceNumber) + ".pdf"

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// isPDFDataIncompleteError reports whether err is one of
// InvoicePDFService's business-data-incompleteness sentinels.
func isPDFDataIncompleteError(err error) bool {
	switch {
	case errors.Is(err, ErrInvoicePDFSellerNameMissing),
		errors.Is(err, ErrInvoicePDFCustomerNameMissing),
		errors.Is(err, ErrInvoicePDFCurrencyMissing),
		errors.Is(err, ErrInvoicePDFCurrencyInvalid),
		errors.Is(err, ErrInvoicePDFDataUnavailable):
		return true
	default:
		return false
	}
}

// isSnapshotIncompleteError reports whether err is one of Send's
// business-data-incompleteness sentinels (Milestone 7 Part 2) — an
// invoice that exists and is a valid Draft, but cannot currently be
// finalised because the organisation/customer/settings data its
// immutable snapshot depends on is missing or incomplete. These all map
// to 409, the same category as an already-Sent invoice, not a generic
// 500 — none of them leak any database or library internals, since every
// one is this package's own sentinel with an already-safe message.
func isSnapshotIncompleteError(err error) bool {
	switch {
	case errors.Is(err, ErrInvoiceSettingsNotFound),
		errors.Is(err, ErrInvoiceSnapshotDataUnavailable),
		errors.Is(err, ErrInvoiceSnapshotSellerNameRequired),
		errors.Is(err, ErrInvoiceSnapshotCustomerNameRequired),
		errors.Is(err, ErrInvoiceSnapshotCurrencyRequired),
		errors.Is(err, ErrInvoiceSnapshotCurrencyInvalid):
		return true
	default:
		return false
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
