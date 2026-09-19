package invoice

import (
	"errors"
	"strings"
)

// Snapshot content-validation errors (Milestone 7 Part 2). These guard
// InvoicePartySnapshot itself — distinct from ErrInvoiceSnapshotDataUnavailable
// and ErrInvoiceSettingsNotFound (invoice_service.go), which guard
// whether the source records could be loaded at all.
var (
	ErrInvoiceSnapshotSellerNameRequired   = errors.New("invoice snapshot seller name is required")
	ErrInvoiceSnapshotCustomerNameRequired = errors.New("invoice snapshot customer name is required")
	ErrInvoiceSnapshotCurrencyRequired     = errors.New("invoice snapshot currency is required")
	ErrInvoiceSnapshotCurrencyInvalid      = errors.New("invoice snapshot currency must be a 3-letter ISO-style code")
)

// InvoicePartySnapshot is the immutable seller/customer/currency snapshot
// captured exactly once, at Draft -> Sent (see Invoice.MarkSent) — never
// at invoice creation, and never recaptured on a repeat Send. It is built
// directly from domain/repository data (Organisation, Customer, the
// customer's billing Address, and Settings.Currency), never from an HTTP
// DTO, JSON, or any presentation-layer representation — see
// InvoiceService.Send for exactly where each field comes from.
//
// SellerName, CustomerName and Currency are required: Validate rejects a
// snapshot missing any of them, since a historical invoice document
// cannot meaningfully exist without knowing who it's from, who it's to,
// or what currency its amounts are in. Every other field is optional and
// left as a nil pointer when the source data doesn't have it — exactly
// the same "pointer means nullable/absent" convention already used
// throughout this codebase (Organisation.Email, Customer.Phone, ...) —
// including every customer address field, since a customer may have no
// billing address at all (see CustomerAddress's own comment).
//
// Raw values only: no money formatting, no address concatenation, no
// pre-formatted display strings. Presentation formatting is Part 3's
// concern, not this one's.
type InvoicePartySnapshot struct {
	SellerName       string
	SellerEmail      *string
	SellerPhone      *string
	SellerWebsite    *string
	SellerAddress    *string
	SellerCity       *string
	SellerState      *string
	SellerPostalCode *string
	SellerCountry    *string
	SellerTaxID      *string

	CustomerName        string
	CustomerCompanyName *string
	CustomerEmail       *string
	CustomerPhone       *string
	CustomerTaxID       *string

	// CustomerAddress/City/State/PostalCode/Country come from the
	// customer's billing address (customer.AddressRepository), not the
	// customer record itself. All five are nil together when the
	// customer has no billing address at all — Milestone 7 Part 1 never
	// required one to exist, and Send must still succeed without one
	// (see InvoiceService.buildPartySnapshot).
	CustomerAddress    *string
	CustomerCity       *string
	CustomerState      *string
	CustomerPostalCode *string
	CustomerCountry    *string

	Currency string
}

// Validate enforces the three required fields and the currency's basic
// shape. It does not trim or normalize — InvoiceService.Send builds this
// value from already-trimmed/normalized source data (Organisation.Name,
// Customer.Name, and an explicitly upper-cased/trimmed Currency), so
// Validate's job is only to confirm the result is usable, not to clean it
// up.
func (s InvoicePartySnapshot) Validate() error {
	if s.SellerName == "" {
		return ErrInvoiceSnapshotSellerNameRequired
	}

	if s.CustomerName == "" {
		return ErrInvoiceSnapshotCustomerNameRequired
	}

	if s.Currency == "" {
		return ErrInvoiceSnapshotCurrencyRequired
	}

	if !isValidCurrencyCode(s.Currency) {
		return ErrInvoiceSnapshotCurrencyInvalid
	}

	return nil
}

// isValidCurrencyCode reports whether code is a 3-letter uppercase
// ISO-4217-shaped code (e.g. "GBP", "EUR", "USD") — no currency
// catalogue, no validity-against-a-real-list check, just the minimal
// shape invariant needed before persisting it as a snapshot value.
func isValidCurrencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}

	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}

// normalizeCurrency trims and upper-cases a raw currency code — the one
// piece of normalization this milestone introduces, applied once when
// InvoiceService.Send reads Settings.Currency, so Validate can check a
// consistent shape rather than every call site re-implementing this.
func normalizeCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}
