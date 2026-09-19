package invoice

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/httpx"
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
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	inv, lines, currency, err := h.service.Create(r.Context(), identity.OrganisationID, request)
	if err != nil {
		if errors.Is(err, ErrInvoiceCustomerNotFound) || errors.Is(err, ErrInvoiceLineProductNotFound) {
			httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, err.Error())
			return
		}

		if isInvoiceValidationError(err) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to create invoice")
		return
	}

	// A brand-new invoice cannot have any payments yet — no query needed
	// to know amountPaid is 0.
	response := toInvoiceResponse(inv, lines, 0, currency, time.Now().UTC())

	httpx.WriteJSON(w, http.StatusCreated, response)
}

// GetByID handles GET /invoices/{id}.
func (h *InvoiceHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid invoice ID")
		return
	}

	inv, lines, amountPaid, currency, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "invoice_not_found", "invoice not found")
			return
		}

		if errors.Is(err, ErrInvoiceCurrencyUnavailable) {
			httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get invoice")
		return
	}

	response := toInvoiceResponse(inv, lines, amountPaid, currency, time.Now().UTC())

	httpx.WriteJSON(w, http.StatusOK, response)
}

// invoiceSortFields is the public sort-field allow-list for GET
// /invoices, validated by httpx.ParseSortOrder before List ever runs —
// see invoiceSortColumns in invoice_repository_postgres.go for how each
// of these maps onto an actual SQL column.
var invoiceSortFields = []string{"invoiceNumber", "issueDate", "dueDate", "total", "createdAt"}

// List handles GET /invoices (Milestone 8 Part 3) — available to every
// authenticated role, same policy as every other invoice route. Default
// sort is issueDate descending: an invoice dashboard is most usefully
// browsed newest-issued-first by default.
//
// now is captured exactly once, here, and threaded through both
// InvoiceService.List's effective-status filtering and every row's own
// EffectiveStatus/currency resolution — see InvoiceService.List's own
// comment for why a single shared value matters.
func (h *InvoiceHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	if !httpx.RejectUnknownQueryParams(
		w, r,
		"limit", "offset", "status", "customerId", "search",
		"issueDateFrom", "issueDateTo", "dueDateFrom", "dueDateTo",
		"sort", "order",
	) {
		return
	}

	limit, offset, ok := httpx.ParseLimitOffset(w, r)
	if !ok {
		return
	}

	sort, order, ok := httpx.ParseSortOrder(w, r, invoiceSortFields, "issueDate", "desc")
	if !ok {
		return
	}

	query := r.URL.Query()
	status, _ := httpx.OptionalQueryParam(query, "status")
	search, _ := httpx.OptionalQueryParam(query, "search")

	var customerID *uuid.UUID
	if raw, present := httpx.OptionalQueryParam(query, "customerId"); present {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid customerId")
			return
		}
		customerID = &parsed
	}

	issueDateFrom, ok := parseOptionalListDate(w, query, "issueDateFrom")
	if !ok {
		return
	}
	issueDateTo, ok := parseOptionalListDate(w, query, "issueDateTo")
	if !ok {
		return
	}
	dueDateFrom, ok := parseOptionalListDate(w, query, "dueDateFrom")
	if !ok {
		return
	}
	dueDateTo, ok := parseOptionalListDate(w, query, "dueDateTo")
	if !ok {
		return
	}

	filter := InvoiceListFilter{
		Status:        status,
		CustomerID:    customerID,
		Search:        search,
		IssueDateFrom: issueDateFrom,
		IssueDateTo:   issueDateTo,
		DueDateFrom:   dueDateFrom,
		DueDateTo:     dueDateTo,
		Sort:          sort,
		Order:         order,
		Limit:         limit,
		Offset:        offset,
	}

	now := time.Now().UTC()

	items, total, err := h.service.List(r.Context(), identity.OrganisationID, filter, now)
	if err != nil {
		if errors.Is(err, ErrInvoiceListStatusInvalid) ||
			errors.Is(err, ErrInvoiceListIssueDateRangeInvalid) ||
			errors.Is(err, ErrInvoiceListDueDateRangeInvalid) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		if errors.Is(err, ErrInvoiceCurrencyUnavailable) {
			httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to list invoices")
		return
	}

	responses := make([]InvoiceResponse, 0, len(items))
	for _, item := range items {
		// A list row never fetches its own line items — see
		// InvoiceService.List's own comment — so Lines is always empty
		// here; GET /invoices/{id} remains the way to see full line
		// detail for one invoice.
		responses = append(responses, toInvoiceResponse(item.Invoice, nil, item.AmountPaid, item.Currency, now))
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.NewListResponse(responses, limit, offset, total))
}

// parseOptionalListDate reads key from query as an optional "YYYY-MM-DD"
// date, returning ok=false (after writing a 400) if it's present but
// malformed. A missing/blank value returns a nil pointer with ok=true —
// "no bound on this side of the range".
func parseOptionalListDate(w http.ResponseWriter, query url.Values, key string) (*time.Time, bool) {
	raw, present := httpx.OptionalQueryParam(query, key)
	if !present {
		return nil, true
	}

	parsed, err := time.Parse(dateLayout, raw)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, key+" must be a YYYY-MM-DD date")
		return nil, false
	}

	return &parsed, true
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
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid invoice ID")
		return
	}

	if _, err := h.service.Send(r.Context(), identity.OrganisationID, id); err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "invoice_not_found", "invoice not found")
			return
		}

		if errors.Is(err, ErrInvoiceAlreadySent) {
			httpx.WriteError(w, http.StatusConflict, "invoice_already_sent", err.Error())
			return
		}

		// Milestone 7 Part 2: the invoice exists and is a valid Draft, but
		// a required snapshot field (seller name, customer name,
		// currency) resolved to blank, or the organisation genuinely has
		// no settings row yet — a 409, the same "exists but can't
		// currently be finalised" category as an already-Sent invoice.
		// This is deliberately narrower than "any related-record lookup
		// failed" (see isSnapshotIncompleteError's own comment for why
		// ErrInvoiceSnapshotDataUnavailable is excluded) — only sentinels
		// with a fixed, safe message reach this branch.
		if isSnapshotIncompleteError(err) {
			httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to send invoice")
		return
	}

	// Send only returns the bare *Invoice; re-fetch through the existing
	// GetByID path to build the same full InvoiceResponse shape (lines +
	// amountPaid + currency) every other invoice-returning endpoint
	// already uses, rather than duplicating that assembly here. A
	// newly-sent invoice cannot have any payments yet (CreatePayment
	// already refuses a Draft invoice), so this adds no surprising
	// state, just the lines; its currency snapshot was just captured by
	// Send itself, so GetByID's resolution can't fail here in practice.
	inv, lines, amountPaid, currency, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get invoice")
		return
	}

	response := toInvoiceResponse(inv, lines, amountPaid, currency, time.Now().UTC())

	httpx.WriteJSON(w, http.StatusOK, response)
}

// CreatePayment handles POST /invoices/{id}/payments.
func (h *InvoiceHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	invoiceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid invoice ID")
		return
	}

	var body CreatePaymentHTTPRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}

	request, err := body.toCreatePaymentRequest()
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, "invalid payment date")
		return
	}

	payment, _, err := h.service.CreatePayment(r.Context(), identity.OrganisationID, invoiceID, request)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "invoice_not_found", "invoice not found")
			return
		}

		if errors.Is(err, ErrInvoiceCannotAcceptPayment) {
			httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, err.Error())
			return
		}

		// Milestone 8 Part 2 (400 vs 409 convention): the amount is
		// intrinsically invalid (<= 0) regardless of any server state —
		// that stays 400. Exceeding the outstanding balance depends
		// entirely on the invoice's current state (how much has already
		// been paid), so it's a conflict with that state, not a
		// malformed request — 409, not 400.
		if errors.Is(err, ErrPaymentAmountInvalid) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		if errors.Is(err, ErrPaymentExceedsOutstanding) {
			httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to create payment")
		return
	}

	response := toPaymentResponse(payment)

	httpx.WriteJSON(w, http.StatusCreated, response)
}

// GetPayments handles GET /invoices/{id}/payments.
func (h *InvoiceHandler) GetPayments(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	invoiceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid invoice ID")
		return
	}

	payments, err := h.service.GetPayments(r.Context(), identity.OrganisationID, invoiceID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "invoice_not_found", "invoice not found")
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get payments")
		return
	}

	response := make([]PaymentResponse, 0, len(payments))
	for _, p := range payments {
		response = append(response, toPaymentResponse(p))
	}

	httpx.WriteJSON(w, http.StatusOK, response)
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
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid invoice ID")
		return
	}

	pdfBytes, invoiceNumber, err := h.pdfService.Generate(r.Context(), identity.OrganisationID, invoiceID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "invoice_not_found", "invoice not found")
			return
		}

		// Milestone 7 Part 3 section 6: an issued invoice missing a
		// required snapshot field, or a Draft invoice missing required
		// live seller/customer/currency data, both mean "this invoice
		// exists but cannot currently produce a historically reliable
		// document" — the same 409 category Send already uses for
		// business-data incompleteness. This is deliberately narrower
		// than "any related-record lookup failed" — see
		// isPDFDataIncompleteError's own comment for why
		// ErrInvoicePDFDataUnavailable is excluded and falls through to
		// the generic 500 below instead.
		if isPDFDataIncompleteError(err) {
			httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, err.Error())
			return
		}

		// Renderer failures, a genuine organisation/customer/settings
		// lookup failure (ErrInvoicePDFDataUnavailable), and any other
		// unexpected repository error are all genuine server-side
		// problems — mapped to a single generic message so no gopdf
		// error, SQL detail, or filesystem path is ever exposed to the
		// client.
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to generate invoice pdf")
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
// InvoicePDFService's business-data-incompleteness sentinels — each of
// these carries a fixed, safe message with no wrapped underlying detail.
// ErrInvoicePDFDataUnavailable is deliberately NOT included here (a
// Milestone 7 hardening-pass fix): it wraps whatever the underlying
// organisation/customer/settings repository call actually failed with
// (via %w: ...: %v), which — unlike the sentinels below — could be a raw
// pgx/SQL error for a genuine transient failure, not just "not found".
// Exposing that via a 409's err.Error() would leak internal detail and
// mischaracterise a real server-side problem as a client-fixable one, so
// it falls through to the generic 500 branch instead.
func isPDFDataIncompleteError(err error) bool {
	switch {
	case errors.Is(err, ErrInvoicePDFSellerNameMissing),
		errors.Is(err, ErrInvoicePDFCustomerNameMissing),
		errors.Is(err, ErrInvoicePDFCurrencyMissing),
		errors.Is(err, ErrInvoicePDFCurrencyInvalid):
		return true
	default:
		return false
	}
}

// isSnapshotIncompleteError reports whether err is one of Send's
// business-data-incompleteness sentinels (Milestone 7 Part 2) — an
// invoice that exists and is a valid Draft, but a required snapshot
// field resolved to blank, or the organisation has no settings row yet.
// Every sentinel matched here carries a fixed, safe message.
// ErrInvoiceSnapshotDataUnavailable is deliberately NOT included (a
// Milestone 7 hardening-pass fix, mirroring isPDFDataIncompleteError's
// identical exclusion): it wraps the underlying repository error via %v,
// which could be a raw pgx/SQL error for a genuine transient failure —
// exposing that through a 409 would both leak internal detail and
// mislabel a real server-side problem, so it falls through to Send's own
// generic 500 branch instead.
func isSnapshotIncompleteError(err error) bool {
	switch {
	case errors.Is(err, ErrInvoiceSettingsNotFound),
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
