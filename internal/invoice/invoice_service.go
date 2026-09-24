package invoice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/product"
)

var (
	// Customer reference.
	ErrInvoiceCustomerIDRequired = errors.New("invoice customer ID is required")
	ErrInvoiceCustomerIDInvalid  = errors.New("invoice customer ID is not a valid UUID")
	ErrInvoiceCustomerNotFound   = errors.New("invoice customer not found")

	// Lines.
	ErrInvoiceNoLines = errors.New("invoice must have at least one line")

	// Dates.
	ErrInvoiceIssueDateRequired      = errors.New("invoice issue date is required")
	ErrInvoiceIssueDateInvalid       = errors.New("invoice issue date is not a valid date")
	ErrInvoiceDueDateRequired        = errors.New("invoice due date is required")
	ErrInvoiceDueDateInvalid         = errors.New("invoice due date is not a valid date")
	ErrInvoiceDueDateBeforeIssueDate = errors.New("invoice due date cannot be before the issue date")

	// Per-line validation.
	ErrInvoiceLineDescriptionRequired = errors.New("invoice line description is required")
	ErrInvoiceLineQuantityInvalid     = errors.New("invoice line quantity must be greater than zero")
	ErrInvoiceLineUnitPriceNegative   = errors.New("invoice line unit price cannot be negative")
	ErrInvoiceLineVATRateNegative     = errors.New("invoice line VAT rate cannot be negative")
	ErrInvoiceLineVATNotPermitted     = errors.New("invoice line cannot charge VAT because the organisation is not VAT registered")
	ErrInvoiceLineProductIDInvalid    = errors.New("invoice line product ID is not a valid UUID")
	ErrInvoiceLineProductNotFound     = errors.New("invoice line product not found")

	// Invoice number allocation. Reused by Send (Milestone 7 Part 2) for
	// the identical "this organisation has no settings row" case when
	// reading Settings.Currency for the snapshot.
	ErrInvoiceSettingsNotFound = errors.New("organisation settings not found")

	// Payments. ErrPaymentAmountInvalid (amount must be > 0) is owned by
	// Payment.Validate in payment.go, not redefined here.
	ErrPaymentExceedsOutstanding = errors.New("payment amount exceeds the invoice's outstanding balance")

	// ErrInvoiceSnapshotDataUnavailable is returned by Send when the
	// organisation itself, or the invoice's customer, cannot be loaded
	// while building the immutable party snapshot (Milestone 7 Part 2) —
	// an extremely unusual state (the organisation is the caller's own
	// authenticated tenant, and the customer was already confirmed to
	// exist within it when the invoice was created), but one Send must
	// still refuse to finalise into rather than persist an incomplete
	// snapshot. Distinct from ErrInvoiceSettingsNotFound, which already
	// has its own established meaning and is reused as-is for the
	// analogous missing-settings case.
	ErrInvoiceSnapshotDataUnavailable = errors.New("required business data for the invoice snapshot is unavailable")

	// ErrInvoiceCurrencyUnavailable is returned when the currency to show
	// on an InvoiceResponse (Milestone 8 Part 2) cannot be reliably
	// determined: a Draft invoice whose organisation has no settings row,
	// or an issued (Sent/Paid) invoice with no immutable currency
	// snapshot — only possible for an invoice sent before Milestone 7
	// Part 2 introduced that column. Deliberately never substituted with
	// a default or with the organisation's current Settings.Currency for
	// an issued invoice — see resolveInvoiceCurrency's own comment.
	ErrInvoiceCurrencyUnavailable = errors.New("invoice currency is unavailable")

	// List filter validation (Milestone 8 Part 3). ErrInvoiceListStatusInvalid
	// covers an unrecognised ?status= value; the four legitimate ones
	// (draft/sent/overdue/paid) are handled entirely explicitly in
	// InvoiceService.List and PostgresInvoiceRepository.List — see
	// section 9's own design note on why "overdue" is never persisted.
	ErrInvoiceListStatusInvalid         = errors.New("invoice status filter is not valid")
	ErrInvoiceListIssueDateRangeInvalid = errors.New("issueDateFrom must not be after issueDateTo")
	ErrInvoiceListDueDateRangeInvalid   = errors.New("dueDateFrom must not be after dueDateTo")
)

// TxBeginner starts a new transaction. *pgxpool.Pool satisfies this
// directly — its Begin method already has this exact signature — so
// InvoiceService can depend on this narrow interface (and be given a fake
// in tests) instead of the whole pool API. Using it is what lets the
// service itself own the transaction's Begin/Commit/Rollback calls, rather
// than the repository.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// InvoiceService sits between the HTTP layer and the invoice repository.
// It also depends on the CustomerRepository and ProductRepository
// interfaces (not concrete implementations) solely to check — within the
// requesting organisation — that a referenced customer or product exists;
// it does not otherwise read or mutate customer/product data (outside of
// Send's snapshot capture — see below), and it never reads a product's
// price. It depends on SettingsRepository to allocate each invoice's
// sequential number, and on PaymentRepository for CreatePayment —
// payments are treated as part of the invoice aggregate rather than a
// separate service, since recording one is inseparable from checking and
// possibly updating the owning invoice's own state.
//
// OrganisationRepository and AddressRepository (Milestone 7 Part 2) exist
// solely for Send's immutable party-snapshot capture: reading the
// organisation's own party details and the invoice's customer's billing
// address, once, at the Draft -> Sent transition. Nothing else in this
// service touches either.
type InvoiceService struct {
	repository             InvoiceRepository
	customerRepository     customer.CustomerRepository
	productRepository      product.ProductRepository
	organisationRepository admin.OrganisationRepository
	addressRepository      customer.AddressRepository
	settingsRepository     admin.SettingsRepository
	paymentRepository      PaymentRepository
	txBeginner             TxBeginner
}

func NewInvoiceService(
	repository InvoiceRepository,
	customerRepository customer.CustomerRepository,
	productRepository product.ProductRepository,
	organisationRepository admin.OrganisationRepository,
	addressRepository customer.AddressRepository,
	settingsRepository admin.SettingsRepository,
	paymentRepository PaymentRepository,
	txBeginner TxBeginner,
) *InvoiceService {
	return &InvoiceService{
		repository:             repository,
		customerRepository:     customerRepository,
		productRepository:      productRepository,
		organisationRepository: organisationRepository,
		addressRepository:      addressRepository,
		settingsRepository:     settingsRepository,
		paymentRepository:      paymentRepository,
		txBeginner:             txBeginner,
	}
}

// validatedLine is the result of structurally validating one
// CreateInvoiceLineRequest, before any database lookup.
type validatedLine struct {
	productID   *uuid.UUID
	description string
	quantity    float64
	unitPrice   int64
	vatRate     float64
}

// Create validates the request, verifies the referenced customer (and any
// referenced products) exist within the organisation, computes every
// line's VAT amount and total plus the invoice's subtotal/VAT total/total,
// and persists the invoice and its lines. The new invoice is returned
// together with its lines and the currency an InvoiceResponse should
// display for it (Milestone 8 Part 2) — always the organisation's
// current Settings.Currency, since a brand-new invoice is always Draft
// and has no snapshot yet.
//
// Structural validation (presence, format, per-field rules) runs before
// any database lookup, so a malformed request never reaches the database.
//
// The invoice number is allocated from the organisation's settings row
// (InvoicePrefix + Settings.NextInvoiceNumber) inside the same
// transaction as the invoice/line inserts, after taking a FOR UPDATE lock
// on that settings row. Holding the lock for the rest of the transaction
// is what makes concurrent invoice creation for the same organisation
// safe: a second, concurrent call blocks at the lock acquisition step
// until the first transaction commits or rolls back, so two invoices for
// the same organisation can never be allocated the same number.
//
// The invoice row, every one of its lines, and the settings update are
// all written inside that single database transaction: if any of them
// fails, or if the commit itself fails, everything from this call is
// rolled back — including the invoice number allocation — so a failed
// creation never consumes a number.
func (s *InvoiceService) Create(
	ctx context.Context,
	organisationID uuid.UUID,
	request CreateInvoiceRequest,
) (*Invoice, []*Line, string, error) {
	customerID, err := parseRequiredUUID(request.CustomerID, ErrInvoiceCustomerIDRequired, ErrInvoiceCustomerIDInvalid)
	if err != nil {
		return nil, nil, "", err
	}

	if len(request.Lines) == 0 {
		return nil, nil, "", ErrInvoiceNoLines
	}

	issueDate, err := parseRequiredDate(request.IssueDate, ErrInvoiceIssueDateRequired, ErrInvoiceIssueDateInvalid)
	if err != nil {
		return nil, nil, "", err
	}

	dueDate, err := parseRequiredDate(request.DueDate, ErrInvoiceDueDateRequired, ErrInvoiceDueDateInvalid)
	if err != nil {
		return nil, nil, "", err
	}

	if dueDate.Before(issueDate) {
		return nil, nil, "", ErrInvoiceDueDateBeforeIssueDate
	}

	validated, err := validateLines(request.Lines)
	if err != nil {
		return nil, nil, "", err
	}

	// Existence checks happen only after every structural rule above has
	// passed, so a malformed request never triggers a database lookup.
	if _, err := s.customerRepository.GetByID(ctx, organisationID, customerID); err != nil {
		if errors.Is(err, customer.ErrCustomerNotFound) {
			return nil, nil, "", ErrInvoiceCustomerNotFound
		}

		return nil, nil, "", fmt.Errorf("look up invoice customer: %w", err)
	}

	// The organisation's VAT status is captured onto the invoice here and
	// never changes afterwards. A business that isn't VAT registered must
	// not charge VAT, so any non-zero rate is rejected rather than
	// silently zeroed.
	organisation, err := s.organisationRepository.GetByID(ctx, organisationID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("look up invoice organisation: %w", err)
	}

	if !organisation.VATRegistered {
		for _, v := range validated {
			if v.vatRate != 0 {
				return nil, nil, "", ErrInvoiceLineVATNotPermitted
			}
		}
	}

	for _, v := range validated {
		if v.productID == nil {
			continue
		}

		if _, err := s.productRepository.GetByID(ctx, organisationID, *v.productID); err != nil {
			if errors.Is(err, product.ErrProductNotFound) {
				return nil, nil, "", ErrInvoiceLineProductNotFound
			}

			return nil, nil, "", fmt.Errorf("look up invoice line product: %w", err)
		}
	}

	invoiceID := uuid.New()

	lines := make([]*Line, 0, len(validated))
	for _, v := range validated {
		vatAmount, total := calculateLineAmounts(v.quantity, v.unitPrice, v.vatRate)

		lines = append(lines, &Line{
			ID:          uuid.New(),
			InvoiceID:   invoiceID,
			ProductID:   v.productID,
			Description: v.description,
			Quantity:    v.quantity,
			UnitPrice:   v.unitPrice,
			VATRate:     v.vatRate,
			VATAmount:   vatAmount,
			Total:       total,
		})
	}

	subtotal, vatTotal, total := sumInvoiceTotals(lines)

	// BEGIN — everything from here to the matching Commit/Rollback below
	// is one atomic unit: locking and incrementing the settings row, the
	// invoice INSERT, and every line INSERT either all succeed together
	// or are all undone together.
	tx, err := s.txBeginner.Begin(ctx)
	if err != nil {
		return nil, nil, "", fmt.Errorf("begin invoice transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	txSettingsRepository := s.settingsRepository.WithTx(tx)

	// FOR UPDATE: locks the settings row until this transaction commits
	// or rolls back, so a concurrent Create for the same organisation
	// cannot read the same "last allocated number" this call is about to
	// increment.
	settings, err := txSettingsRepository.GetForUpdate(ctx, organisationID)
	if err != nil {
		if errors.Is(err, admin.ErrSettingsNotFound) {
			return nil, nil, "", ErrInvoiceSettingsNotFound
		}

		return nil, nil, "", fmt.Errorf("lock organisation settings: %w", err)
	}

	// A Draft invoice always displays the organisation's current
	// Settings.Currency (Milestone 8 Part 2) — there is no snapshot yet
	// to protect, and this same settings row is already locked and in
	// scope for invoice-number allocation, so no extra lookup is needed.
	currency := normalizeCurrency(settings.Currency)

	allocatedNumber := settings.NextInvoiceNumber()

	if err := txSettingsRepository.UpdateInvoiceNumber(ctx, organisationID, settings.InvoiceNumber); err != nil {
		return nil, nil, "", fmt.Errorf("update organisation settings: %w", err)
	}

	inv := &Invoice{
		ID:             invoiceID,
		OrganisationID: organisationID,
		CustomerID:     customerID,
		InvoiceNumber:  settings.InvoicePrefix + strconv.Itoa(allocatedNumber),
		IssueDate:      issueDate,
		DueDate:        dueDate,
		Subtotal:       subtotal,
		VATTotal:       vatTotal,
		Total:          total,
		Status:         InvoiceStatusDraft,
		Notes:          nilIfEmpty(request.Notes),
		VATRegistered:  organisation.VATRegistered,
	}

	txRepository := s.repository.WithTx(tx)

	if err := txRepository.Create(ctx, inv); err != nil {
		return nil, nil, "", err
	}

	if err := txRepository.CreateLines(ctx, lines); err != nil {
		return nil, nil, "", err
	}

	// COMMIT — only reached once the settings update, the invoice, and
	// every line inserted without error.
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, "", fmt.Errorf("commit invoice transaction: %w", err)
	}

	return inv, lines, currency, nil
}

// GetByID delegates the invoice lookup to the repository (organisation
// scoping happens there), fetches its lines, and returns the total
// already paid against it — reusing the same PaymentRepository.
// GetTotalPaidByInvoiceID query CreatePayment already relies on, no new
// SQL. amountOutstanding is not computed here; it's derived by
// toInvoiceResponse from Invoice.Total and this returned amountPaid, the
// same way CreatePayment derives its own outstanding balance.
//
// The returned currency (Milestone 8 Part 2) follows the same binary
// snapshot rule as PDF generation (see InvoicePDFService.BuildData) and
// Send's own snapshot capture: a persisted Draft invoice always shows
// the organisation's current Settings.Currency; anything else (Sent,
// Paid, or effectively Overdue — still persisted Sent) shows ONLY the
// immutable Invoice.Currency snapshot, never live settings, even if the
// organisation's currency has since changed. See resolveInvoiceCurrency.
func (s *InvoiceService) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (*Invoice, []*Line, int64, string, error) {
	inv, err := s.repository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, nil, 0, "", err
	}

	lines, err := s.repository.GetLinesByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, nil, 0, "", err
	}

	amountPaid, err := s.paymentRepository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, nil, 0, "", err
	}

	currency, err := s.resolveInvoiceCurrency(ctx, organisationID, inv)
	if err != nil {
		return nil, nil, 0, "", err
	}

	return inv, lines, amountPaid, currency, nil
}

// resolveInvoiceCurrency implements Milestone 8 Part 2's currency rule
// for GetByID (Create resolves it inline instead, since it already holds
// the relevant settings row inside its own transaction — see Create's
// own comment). A persisted Draft invoice has no snapshot yet, so its
// currency is read live; any other status uses ONLY the immutable
// Invoice.Currency snapshot — see resolveCurrencyFromSettings for the
// pure rule itself, shared with List below so both call sites can never
// silently drift apart on what "resolve this invoice's currency" means.
func (s *InvoiceService) resolveInvoiceCurrency(
	ctx context.Context,
	organisationID uuid.UUID,
	inv *Invoice,
) (string, error) {
	if inv.Status != InvoiceStatusDraft {
		return resolveCurrencyFromSettings(inv, nil)
	}

	settings, err := s.settingsRepository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		if errors.Is(err, admin.ErrSettingsNotFound) {
			return "", ErrInvoiceCurrencyUnavailable
		}

		return "", fmt.Errorf("look up organisation settings for invoice currency: %w", err)
	}

	return resolveCurrencyFromSettings(inv, settings)
}

// resolveCurrencyFromSettings is the pure currency-resolution rule,
// factored out of resolveInvoiceCurrency so InvoiceService.List can reuse
// it without a settings lookup per row (see List's own comment for why
// settings is fetched at most once per page, not once per invoice).
// settings may be nil — that's only ever valid for a non-Draft inv,
// where it's never consulted at all; a nil settings for a Draft inv (no
// settings row exists for the organisation) is exactly
// ErrInvoiceCurrencyUnavailable, the same as a Draft's live lookup
// failing.
//
// An invoice sent before the Currency column existed has Currency == nil
// despite not being Draft — that is deliberately reported as
// ErrInvoiceCurrencyUnavailable rather than silently shown using the
// organisation's current currency, which could misrepresent a legacy
// invoice's actual historical currency.
func resolveCurrencyFromSettings(inv *Invoice, settings *admin.Settings) (string, error) {
	if inv.Status != InvoiceStatusDraft {
		if inv.Currency == nil || strings.TrimSpace(*inv.Currency) == "" {
			return "", ErrInvoiceCurrencyUnavailable
		}

		return normalizeCurrency(*inv.Currency), nil
	}

	if settings == nil {
		return "", ErrInvoiceCurrencyUnavailable
	}

	return normalizeCurrency(settings.Currency), nil
}

// InvoiceListItem is one row of InvoiceService.List's result: the
// invoice itself, plus the two values toInvoiceResponse needs but that
// aren't columns on the invoice row — AmountPaid (from the batched
// payment-total query) and Currency (resolved per the same binary rule
// GetByID uses). List never fetches per-invoice line items — see List's
// own comment for why a list row's Lines is always empty.
type InvoiceListItem struct {
	Invoice    *Invoice
	AmountPaid int64
	Currency   string
}

// List validates filter.Status and any supplied date-range ordering,
// then delegates the actual row selection to the repository — which
// enforces tenant scoping and every other predicate in SQL — before
// assembling each row's AmountPaid and Currency without either becoming
// an N+1 query pattern (Milestone 8 Part 3 sections 10-11):
//
//   - Settings.Currency is fetched at most ONCE per call (not once per
//     Draft row) — every Draft row in the page shares the same
//     organisation, so one lookup covers all of them.
//   - Payment totals are fetched in a single batched, grouped query
//     keyed by every invoice ID on the page (GetTotalPaidByInvoiceIDs),
//     never one query per invoice.
//
// now flows through unchanged from the HTTP handler (see GetByID's own
// reasoning for why this stays a parameter rather than a time.Now()
// call here) and is truncated to a UTC calendar date once
// (invoice.UTCDate) for the repository's effective-overdue predicate —
// the same "today" every row's own EffectiveStatus(now) call in
// toInvoiceResponse will independently arrive at, so a row selected as
// "overdue" can never come back displaying "sent" or vice versa.
//
// A legacy issued invoice missing its currency snapshot fails the whole
// request (ErrInvoiceCurrencyUnavailable) rather than silently omitting
// that one row or fabricating a currency for it — the same
// fail-safely-rather-than-lie choice GetByID already makes for a single
// invoice, applied consistently to a list containing one.
func (s *InvoiceService) List(
	ctx context.Context,
	organisationID uuid.UUID,
	filter InvoiceListFilter,
	now time.Time,
) ([]InvoiceListItem, int64, error) {
	switch filter.Status {
	case "", InvoiceStatusDraft, InvoiceStatusSent, InvoiceStatusOverdue, InvoiceStatusPaid:
	default:
		return nil, 0, ErrInvoiceListStatusInvalid
	}

	if filter.IssueDateFrom != nil && filter.IssueDateTo != nil && filter.IssueDateFrom.After(*filter.IssueDateTo) {
		return nil, 0, ErrInvoiceListIssueDateRangeInvalid
	}
	if filter.DueDateFrom != nil && filter.DueDateTo != nil && filter.DueDateFrom.After(*filter.DueDateTo) {
		return nil, 0, ErrInvoiceListDueDateRangeInvalid
	}

	today := UTCDate(now)

	invoices, total, err := s.repository.List(ctx, organisationID, filter, today)
	if err != nil {
		return nil, 0, err
	}

	if len(invoices) == 0 {
		return []InvoiceListItem{}, total, nil
	}

	// Fetch Settings.Currency at most once, only if the page actually
	// contains a Draft row that needs it.
	var settings *admin.Settings
	for _, inv := range invoices {
		if inv.Status == InvoiceStatusDraft {
			settings, err = s.settingsRepository.GetByOrganisationID(ctx, organisationID)
			if err != nil {
				if errors.Is(err, admin.ErrSettingsNotFound) {
					return nil, 0, ErrInvoiceCurrencyUnavailable
				}

				return nil, 0, fmt.Errorf("look up organisation settings for invoice list currency: %w", err)
			}

			break
		}
	}

	ids := make([]uuid.UUID, len(invoices))
	for i, inv := range invoices {
		ids[i] = inv.ID
	}

	amountPaidByID, err := s.paymentRepository.GetTotalPaidByInvoiceIDs(ctx, organisationID, ids)
	if err != nil {
		return nil, 0, fmt.Errorf("get total paid for invoice list: %w", err)
	}

	items := make([]InvoiceListItem, 0, len(invoices))
	for _, inv := range invoices {
		currency, err := resolveCurrencyFromSettings(inv, settings)
		if err != nil {
			return nil, 0, err
		}

		items = append(items, InvoiceListItem{
			Invoice:    inv,
			AmountPaid: amountPaidByID[inv.ID],
			Currency:   currency,
		})
	}

	return items, total, nil
}

// Send finalises a Draft invoice into the Sent lifecycle state — this is
// a lifecycle transition only: it does not generate a PDF, send an
// email, or contact the customer in any way (those belong to a later
// milestone). "Sent" here means "finalised," not "delivered."
//
// BEGIN
//
//	lock invoice row (FOR UPDATE) and verify it belongs to organisationID
//	if not Draft: return ErrInvoiceAlreadySent immediately — nothing below
//	  this line ever runs for a repeat Send (Milestone 7 Part 2): no
//	  Organisation/Customer/Address/Settings read, no snapshot rebuilt
//	load Organisation(organisationID), Customer(organisationID, invoice's
//	  CustomerID), the customer's billing Address (optional), and
//	  Settings(organisationID) — all through this same transaction
//	construct and validate the immutable InvoicePartySnapshot
//	apply the in-memory Draft -> Sent guard + snapshot (Invoice.MarkSent)
//	persist status + sent_at + every snapshot column together
//	  (InvoiceRepository.MarkSentWithSnapshot)
//
// # COMMIT
//
// The row is locked before any of this runs and held until
// commit/rollback, exactly like CreatePayment's own GetForUpdate usage —
// that's what makes two concurrent Send calls against the same invoice
// safe: the second call blocks at the lock-acquisition step until the
// first transaction finishes, then observes the now-Sent status and
// returns ErrInvoiceAlreadySent rather than double-transitioning,
// recapturing the snapshot, or corrupting state. A rejected transition
// (already Sent, or Paid) never reaches any snapshot-building step or the
// repository write — both guards (the early status check here, and
// MarkSent's own) fail before either one, so nothing is persisted and the
// transaction rolls back having made no change.
//
// organisationID is the caller's trusted tenant identity (ultimately
// AuthenticatedUser.OrganisationID) and is the sole value used to scope
// every one of these reads — never a value read off the invoice row
// itself or any loaded record. A cross-tenant Send fails at the very
// first GetForUpdate call, before any of the snapshot-building code
// below is ever reached.
func (s *InvoiceService) Send(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (*Invoice, error) {
	tx, err := s.txBeginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin send transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepository := s.repository.WithTx(tx)

	inv, err := txRepository.GetForUpdate(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, err
	}

	if inv.Status != InvoiceStatusDraft {
		return nil, ErrInvoiceAlreadySent
	}

	snapshot, err := s.buildPartySnapshot(ctx, tx, organisationID, inv.CustomerID)
	if err != nil {
		return nil, err
	}

	sentAt := time.Now().UTC()

	if err := inv.MarkSent(sentAt, snapshot); err != nil {
		return nil, err
	}

	if err := txRepository.MarkSentWithSnapshot(ctx, organisationID, invoiceID, inv); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit send transaction: %w", err)
	}

	return inv, nil
}

// buildPartySnapshot loads everything Send needs to construct an
// InvoicePartySnapshot — the organisation, the invoice's customer, the
// customer's billing address (if one exists), and the organisation's
// configured currency — all through tx, so they're read within the same
// transaction that already holds the invoice row's FOR UPDATE lock.
//
// A missing billing address is not an error: customer.ErrBillingAddressNotFound
// is the one expected, harmless outcome here (Milestone 7 Part 1 never
// required every customer to have one), and simply leaves every
// CustomerAddress/City/State/PostalCode/Country field nil on the
// resulting snapshot. Any other failure loading the organisation or
// customer is wrapped as ErrInvoiceSnapshotDataUnavailable; a missing
// settings row is reported as ErrInvoiceSettingsNotFound, the same
// sentinel Create already uses for the identical "no settings row"
// situation.
func (s *InvoiceService) buildPartySnapshot(
	ctx context.Context,
	tx pgx.Tx,
	organisationID uuid.UUID,
	customerID uuid.UUID,
) (InvoicePartySnapshot, error) {
	organisation, err := s.organisationRepository.WithTx(tx).GetByID(ctx, organisationID)
	if err != nil {
		return InvoicePartySnapshot{}, fmt.Errorf("%w: look up organisation: %v", ErrInvoiceSnapshotDataUnavailable, err)
	}

	cust, err := s.customerRepository.WithTx(tx).GetByID(ctx, organisationID, customerID)
	if err != nil {
		return InvoicePartySnapshot{}, fmt.Errorf("%w: look up customer: %v", ErrInvoiceSnapshotDataUnavailable, err)
	}

	settings, err := s.settingsRepository.WithTx(tx).GetByOrganisationID(ctx, organisationID)
	if err != nil {
		if errors.Is(err, admin.ErrSettingsNotFound) {
			return InvoicePartySnapshot{}, ErrInvoiceSettingsNotFound
		}

		return InvoicePartySnapshot{}, fmt.Errorf("%w: look up settings: %v", ErrInvoiceSnapshotDataUnavailable, err)
	}

	snapshot := InvoicePartySnapshot{
		SellerName:       organisation.Name,
		SellerEmail:      organisation.Email,
		SellerPhone:      organisation.Phone,
		SellerWebsite:    organisation.Website,
		SellerAddress:    organisation.Address,
		SellerCity:       organisation.City,
		SellerState:      organisation.State,
		SellerPostalCode: organisation.PostalCode,
		SellerCountry:    organisation.Country,
		SellerTaxID:      sellerVATNumber(organisation),

		CustomerName:        cust.Name,
		CustomerCompanyName: cust.CompanyName,
		CustomerEmail:       cust.Email,
		CustomerPhone:       cust.Phone,
		CustomerTaxID:       cust.TaxID,

		Currency: normalizeCurrency(settings.Currency),
	}

	billingAddress, err := s.addressRepository.WithTx(tx).GetBillingAddressByCustomerID(ctx, organisationID, customerID)
	if err != nil {
		if !errors.Is(err, customer.ErrBillingAddressNotFound) {
			return InvoicePartySnapshot{}, fmt.Errorf("%w: look up billing address: %v", ErrInvoiceSnapshotDataUnavailable, err)
		}
		// No billing address yet — acceptable; the customer-address
		// fields on snapshot simply stay nil.
	} else {
		snapshot.CustomerAddress = nilIfEmpty(billingAddress.Street)
		snapshot.CustomerCity = nilIfEmpty(billingAddress.City)
		snapshot.CustomerState = nilIfEmpty(billingAddress.State)
		snapshot.CustomerPostalCode = nilIfEmpty(billingAddress.PostalCode)
		snapshot.CustomerCountry = nilIfEmpty(billingAddress.Country)
	}

	return snapshot, nil
}

// CreatePaymentRequest is the caller-supplied shape for recording a
// payment against an invoice — InvoiceService.CreatePayment's own input.
// POST /invoices/{id}/payments decodes the wire-format
// CreatePaymentHTTPRequest and converts it into this (see
// payment_dto.go's toCreatePaymentRequest).
//
// PaymentDate is a real time.Time rather than a wire-format string: the
// handler parses the request body's "YYYY-MM-DD" date before constructing
// this. A zero PaymentDate (date omitted) defaults to time.Now(); a
// non-zero one is used exactly as supplied.
//
// IdempotencyKey (Milestone 13 Part 1) is required — see
// ValidateIdempotencyKey. It identifies one logical payment attempt
// against this invoice; it is not part of the payment's own data.
type CreatePaymentRequest struct {
	Amount         int64
	PaymentMethod  string
	PaymentDate    time.Time
	Reference      string
	Notes          string
	IdempotencyKey string
}

// CreatePaymentResult is CreatePayment's outcome. Replayed is true when
// no new payment was recorded because this request's IdempotencyKey had
// already produced Payment, with a matching request fingerprint; Invoice
// is then nil, since a replay never reads or changes the invoice's
// payment state (the handler doesn't use it either way).
type CreatePaymentResult struct {
	Payment  *Payment
	Invoice  *Invoice
	Replayed bool
}

// CreatePayment records a payment against an invoice and, if the payment
// exhausts the invoice's outstanding balance, updates its status to
// "paid" — all within a single database transaction:
//
//	validate the idempotency key, fingerprint the request (outside the tx)
//	BEGIN
//	    lock invoice row (FOR UPDATE) and verify it belongs to organisationID
//	    look up a payment already created on this invoice with this key
//	        found, same fingerprint      -> return it as a replay
//	        found, different fingerprint -> ErrPaymentIdempotencyKeyReused
//	    check the invoice can accept a payment
//	    calculate total already paid
//	    validate the new payment does not exceed the outstanding balance
//	    insert the payment, with its idempotency key and fingerprint
//	    update invoice status to "paid" if this payment exhausts the balance
//	COMMIT
//
// The invoice lock is acquired before the outstanding balance is
// calculated, and held until commit/rollback. That ordering is what makes
// concurrent payments against the same invoice safe: a second, concurrent
// call blocks at the lock-acquisition step until the first transaction
// finishes, so two payments can never both be validated against the same
// stale outstanding balance and together overpay the invoice.
//
// Idempotency (Milestone 13 Part 1). The same lock is also the
// serialisation point for retries: a concurrent request carrying the same
// key blocks on GetForUpdate until the first commits, and its key lookup
// — a new statement, so under this transaction's default READ COMMITTED
// isolation it sees everything committed before it began — then finds
// the first request's payment and replays it. (Under REPEATABLE READ or
// SERIALIZABLE the lookup would see only the transaction's original
// snapshot, so this relies on READ COMMITTED.) The lookup deliberately
// runs BEFORE CanAcceptPayment: retrying a payment that settled the
// invoice in full must replay that payment, not fail because the invoice
// is now Paid. The key and fingerprint are written on the payment row
// itself, so they become durable exactly when — and only if — the
// payment does: every failure below (lifecycle, overpayment, database
// error, rollback) leaves the key unused and freely retryable, and an
// ambiguous COMMIT error is resolved by the client retrying with the
// same key (replay if it did commit, a normal execution if it didn't).
// Nothing here ever retries a commit itself.
//
// A fully paid invoice's outstanding balance is 0, so any further
// strictly-positive payment amount already fails the "amount <=
// outstanding" check below — there is no separate "invoice already paid"
// branch, and none is needed. Likewise, no separate "partially paid"
// status exists: a payment that doesn't exhaust the balance is simply
// inserted, and the invoice's existing status (draft, sent, ...) is left
// untouched.
//
// The amount-must-be-positive rule is owned by Payment.Validate and is
// checked before the transaction even begins, since it needs no database
// state. Organisation scoping, the outstanding-balance check, and the
// status update all happen inside the transaction, so a failure anywhere
// rolls back the payment, the status change, or both together — a
// rejected payment is never partially recorded.
func (s *InvoiceService) CreatePayment(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
	request CreatePaymentRequest,
) (CreatePaymentResult, error) {
	if err := ValidateIdempotencyKey(request.IdempotencyKey); err != nil {
		return CreatePaymentResult{}, err
	}

	// Fingerprinted before PaymentDate's time.Now() default below, so an
	// omitted date fingerprints as omitted (see paymentFingerprintV1).
	idempotency := PaymentIdempotency{
		Key:         request.IdempotencyKey,
		RequestHash: fingerprintPaymentRequest(request),
	}

	paymentDate := request.PaymentDate
	if paymentDate.IsZero() {
		paymentDate = time.Now().UTC()
	}

	payment := &Payment{
		ID:            uuid.New(),
		InvoiceID:     invoiceID,
		Amount:        request.Amount,
		PaymentMethod: request.PaymentMethod,
		PaymentDate:   paymentDate,
		Reference:     nilIfEmpty(request.Reference),
		Notes:         nilIfEmpty(request.Notes),
	}

	if err := payment.Validate(); err != nil {
		return CreatePaymentResult{}, err
	}

	// BEGIN — everything from here to the matching Commit/Rollback below
	// is one atomic unit: locking the invoice, the outstanding-balance
	// check, the payment INSERT, and the status update either all succeed
	// together or are all undone together.
	tx, err := s.txBeginner.Begin(ctx)
	if err != nil {
		return CreatePaymentResult{}, fmt.Errorf("begin payment transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	txInvoiceRepository := s.repository.WithTx(tx)

	// FOR UPDATE: locks the invoice row until this transaction commits or
	// rolls back, and — via the same organisation_id predicate GetByID
	// uses — establishes that the invoice belongs to organisationID. A
	// caller cannot pay against another organisation's invoice by knowing
	// its UUID: this returns ErrInvoiceNotFound exactly as GetByID would
	// for an invoice that doesn't exist at all.
	inv, err := txInvoiceRepository.GetForUpdate(ctx, organisationID, invoiceID)
	if err != nil {
		return CreatePaymentResult{}, err
	}

	txPaymentRepository := s.paymentRepository.WithTx(tx)

	// Idempotency lookup: only after the lock (so a concurrent same-key
	// request has already committed or rolled back) and before any
	// lifecycle/balance check (so a replay never depends on the invoice's
	// current state). A replay or conflict writes nothing; the deferred
	// Rollback simply releases the lock.
	existing, existingHash, err := txPaymentRepository.GetByIdempotencyKey(ctx, organisationID, invoiceID, idempotency.Key)
	switch {
	case err == nil:
		if !bytes.Equal(existingHash, idempotency.RequestHash) {
			return CreatePaymentResult{}, ErrPaymentIdempotencyKeyReused
		}

		return CreatePaymentResult{Payment: existing, Replayed: true}, nil
	case !errors.Is(err, errPaymentNotFound):
		return CreatePaymentResult{}, fmt.Errorf("look up idempotent payment: %w", err)
	}

	// Checked only after the lock is held, so a concurrent Send or
	// another payment can't change the invoice's status out from under
	// this decision. Based on persisted Status (Invoice.CanAcceptPayment),
	// deliberately not the derived EffectiveStatus: an invoice the API
	// currently shows a client as "overdue" is still persisted Sent, and
	// must remain payable. Draft (nothing finalised yet) and Paid (already
	// settled, by lifecycle rule rather than by an outstanding-balance
	// coincidence) are both rejected here, before any payment math runs.
	if !inv.CanAcceptPayment() {
		return CreatePaymentResult{}, ErrInvoiceCannotAcceptPayment
	}

	// Computed only after the lock is held, so a concurrent payment
	// against the same invoice cannot read this same total.
	totalPaid, err := txPaymentRepository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return CreatePaymentResult{}, fmt.Errorf("get total paid: %w", err)
	}

	outstanding := inv.Total - totalPaid

	if payment.Amount > outstanding {
		return CreatePaymentResult{}, ErrPaymentExceedsOutstanding
	}

	if err := txPaymentRepository.Create(ctx, organisationID, payment, idempotency); err != nil {
		return CreatePaymentResult{}, err
	}

	if payment.Amount == outstanding {
		if err := txInvoiceRepository.UpdateStatus(ctx, organisationID, invoiceID, InvoiceStatusPaid); err != nil {
			return CreatePaymentResult{}, fmt.Errorf("update invoice status: %w", err)
		}

		inv.Status = InvoiceStatusPaid
	}

	// COMMIT — only reached once the outstanding-balance check passed,
	// the payment inserted, and (if applicable) the status update
	// succeeded. A commit error is returned as-is and never retried here:
	// the outcome may be ambiguous (see the idempotency note above).
	if err := tx.Commit(ctx); err != nil {
		return CreatePaymentResult{}, fmt.Errorf("commit payment transaction: %w", err)
	}

	return CreatePaymentResult{Payment: payment, Invoice: inv}, nil
}

// GetPayments returns every payment recorded against an invoice, in
// whatever order PaymentRepository.GetByInvoiceID returns them (currently
// payment_date, then created_at — this method doesn't re-sort or alter
// that ordering).
//
// Organisation scoping is enforced the same way GetByID enforces it for
// the invoice itself: this first confirms, via the organisation-scoped
// GetByID, that the invoice belongs to organisationID, before ever
// touching the payment repository. A caller cannot list another
// organisation's invoice's payments by knowing its UUID — that lookup
// fails with ErrInvoiceNotFound exactly as GetByID's own callers see.
//
// This is a plain read, so unlike CreatePayment it does not lock the
// invoice row — there is no concurrent mutation to protect against here.
func (s *InvoiceService) GetPayments(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) ([]*Payment, error) {
	if _, err := s.repository.GetByID(ctx, organisationID, invoiceID); err != nil {
		return nil, err
	}

	return s.paymentRepository.GetByInvoiceID(ctx, organisationID, invoiceID)
}

// validateLines applies every per-line structural rule (description,
// quantity, unit price, VAT rate, product ID format) to every requested
// line, stopping at the first failure. It does not touch the database —
// product existence is checked separately, after every line has passed
// this validation.
func validateLines(requests []CreateInvoiceLineRequest) ([]validatedLine, error) {
	validated := make([]validatedLine, 0, len(requests))

	for _, lineRequest := range requests {
		description := strings.TrimSpace(lineRequest.Description)
		if description == "" {
			return nil, ErrInvoiceLineDescriptionRequired
		}

		if lineRequest.Quantity <= 0 {
			return nil, ErrInvoiceLineQuantityInvalid
		}

		if lineRequest.UnitPrice < 0 {
			return nil, ErrInvoiceLineUnitPriceNegative
		}

		if lineRequest.VATRate < 0 {
			return nil, ErrInvoiceLineVATRateNegative
		}

		var productID *uuid.UUID
		if lineRequest.ProductID != nil {
			if trimmed := strings.TrimSpace(*lineRequest.ProductID); trimmed != "" {
				parsed, err := uuid.Parse(trimmed)
				if err != nil {
					return nil, ErrInvoiceLineProductIDInvalid
				}

				productID = &parsed
			}
		}

		validated = append(validated, validatedLine{
			productID:   productID,
			description: description,
			quantity:    lineRequest.Quantity,
			unitPrice:   lineRequest.UnitPrice,
			vatRate:     lineRequest.VATRate,
		})
	}

	return validated, nil
}

// parseRequiredUUID trims and parses a required UUID string, returning
// requiredErr when blank and invalidErr when malformed.
func parseRequiredUUID(value string, requiredErr, invalidErr error) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return uuid.Nil, requiredErr
	}

	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, invalidErr
	}

	return parsed, nil
}

// parseRequiredDate trims and parses a required "YYYY-MM-DD" date string,
// returning requiredErr when blank and invalidErr when malformed.
func parseRequiredDate(value string, requiredErr, invalidErr error) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, requiredErr
	}

	parsed, err := time.Parse(dateLayout, trimmed)
	if err != nil {
		return time.Time{}, invalidErr
	}

	return parsed, nil
}

// nilIfEmpty converts a blank/whitespace-only string into a nil pointer so
// an optional field is stored as SQL NULL rather than an empty string.
func nilIfEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return &value
}

// sellerVATNumber returns the organisation's TaxID for the invoice
// snapshot, or nil when the organisation isn't VAT registered — a stored
// TaxID is kept on the organisation for when it re-registers, but must
// never appear on a non-registered organisation's invoices.
func sellerVATNumber(organisation *admin.Organisation) *string {
	if !organisation.VATRegistered {
		return nil
	}

	return organisation.TaxID
}
