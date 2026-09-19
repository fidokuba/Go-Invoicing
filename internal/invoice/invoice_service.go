package invoice

import (
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
// together with its lines.
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
) (*Invoice, []*Line, error) {
	customerID, err := parseRequiredUUID(request.CustomerID, ErrInvoiceCustomerIDRequired, ErrInvoiceCustomerIDInvalid)
	if err != nil {
		return nil, nil, err
	}

	if len(request.Lines) == 0 {
		return nil, nil, ErrInvoiceNoLines
	}

	issueDate, err := parseRequiredDate(request.IssueDate, ErrInvoiceIssueDateRequired, ErrInvoiceIssueDateInvalid)
	if err != nil {
		return nil, nil, err
	}

	dueDate, err := parseRequiredDate(request.DueDate, ErrInvoiceDueDateRequired, ErrInvoiceDueDateInvalid)
	if err != nil {
		return nil, nil, err
	}

	if dueDate.Before(issueDate) {
		return nil, nil, ErrInvoiceDueDateBeforeIssueDate
	}

	validated, err := validateLines(request.Lines)
	if err != nil {
		return nil, nil, err
	}

	// Existence checks happen only after every structural rule above has
	// passed, so a malformed request never triggers a database lookup.
	if _, err := s.customerRepository.GetByID(ctx, organisationID, customerID); err != nil {
		if errors.Is(err, customer.ErrCustomerNotFound) {
			return nil, nil, ErrInvoiceCustomerNotFound
		}

		return nil, nil, fmt.Errorf("look up invoice customer: %w", err)
	}

	for _, v := range validated {
		if v.productID == nil {
			continue
		}

		if _, err := s.productRepository.GetByID(ctx, organisationID, *v.productID); err != nil {
			if errors.Is(err, product.ErrProductNotFound) {
				return nil, nil, ErrInvoiceLineProductNotFound
			}

			return nil, nil, fmt.Errorf("look up invoice line product: %w", err)
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
		return nil, nil, fmt.Errorf("begin invoice transaction: %w", err)
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
			return nil, nil, ErrInvoiceSettingsNotFound
		}

		return nil, nil, fmt.Errorf("lock organisation settings: %w", err)
	}

	allocatedNumber := settings.NextInvoiceNumber()

	if err := txSettingsRepository.UpdateInvoiceNumber(ctx, organisationID, settings.InvoiceNumber); err != nil {
		return nil, nil, fmt.Errorf("update organisation settings: %w", err)
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
	}

	txRepository := s.repository.WithTx(tx)

	if err := txRepository.Create(ctx, inv); err != nil {
		return nil, nil, err
	}

	if err := txRepository.CreateLines(ctx, lines); err != nil {
		return nil, nil, err
	}

	// COMMIT — only reached once the settings update, the invoice, and
	// every line inserted without error.
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit invoice transaction: %w", err)
	}

	return inv, lines, nil
}

// GetByID delegates the invoice lookup to the repository (organisation
// scoping happens there), fetches its lines, and returns the total
// already paid against it — reusing the same PaymentRepository.
// GetTotalPaidByInvoiceID query CreatePayment already relies on, no new
// SQL. amountOutstanding is not computed here; it's derived by
// toInvoiceResponse from Invoice.Total and this returned amountPaid, the
// same way CreatePayment derives its own outstanding balance.
func (s *InvoiceService) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (*Invoice, []*Line, int64, error) {
	inv, err := s.repository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, nil, 0, err
	}

	lines, err := s.repository.GetLinesByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, nil, 0, err
	}

	amountPaid, err := s.paymentRepository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, nil, 0, err
	}

	return inv, lines, amountPaid, nil
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
		SellerTaxID:      organisation.TaxID,

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
// payment against an invoice. There is no HTTP handler for this yet (that
// belongs to a later step), but the shape matches this project's existing
// request-struct convention (see CreateInvoiceRequest), so a handler can
// reuse it once added.
//
// PaymentDate is a real time.Time rather than a wire-format string, since
// there is no JSON-decoding boundary in front of this yet — a future
// handler would parse the request body's date string before constructing
// this. A zero PaymentDate defaults to time.Now(); a non-zero one is used
// exactly as supplied.
type CreatePaymentRequest struct {
	Amount        int64
	PaymentMethod string
	PaymentDate   time.Time
	Reference     string
	Notes         string
}

// CreatePayment records a payment against an invoice and, if the payment
// exhausts the invoice's outstanding balance, updates its status to
// "paid" — all within a single database transaction:
//
//	BEGIN
//	    lock invoice row (FOR UPDATE) and verify it belongs to organisationID
//	    calculate total already paid
//	    validate the new payment does not exceed the outstanding balance
//	    insert the payment
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
) (*Payment, *Invoice, error) {
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
		return nil, nil, err
	}

	// BEGIN — everything from here to the matching Commit/Rollback below
	// is one atomic unit: locking the invoice, the outstanding-balance
	// check, the payment INSERT, and the status update either all succeed
	// together or are all undone together.
	tx, err := s.txBeginner.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin payment transaction: %w", err)
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
		return nil, nil, err
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
		return nil, nil, ErrInvoiceCannotAcceptPayment
	}

	txPaymentRepository := s.paymentRepository.WithTx(tx)

	// Computed only after the lock is held, so a concurrent payment
	// against the same invoice cannot read this same total.
	totalPaid, err := txPaymentRepository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, nil, fmt.Errorf("get total paid: %w", err)
	}

	outstanding := inv.Total - totalPaid

	if payment.Amount > outstanding {
		return nil, nil, ErrPaymentExceedsOutstanding
	}

	if err := txPaymentRepository.Create(ctx, organisationID, payment); err != nil {
		return nil, nil, err
	}

	if payment.Amount == outstanding {
		if err := txInvoiceRepository.UpdateStatus(ctx, organisationID, invoiceID, InvoiceStatusPaid); err != nil {
			return nil, nil, fmt.Errorf("update invoice status: %w", err)
		}

		inv.Status = InvoiceStatusPaid
	}

	// COMMIT — only reached once the outstanding-balance check passed,
	// the payment inserted, and (if applicable) the status update
	// succeeded.
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit payment transaction: %w", err)
	}

	return payment, inv, nil
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
