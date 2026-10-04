package invoice

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Invoice status values. Named constants rather than scattered string
// literals — payment processing and lifecycle transitions have real
// status-dependent business logic (CreatePayment sets InvoiceStatusPaid,
// MarkSent guards Draft -> Sent, EffectiveStatus derives Overdue), so a
// typo in a literal would silently produce wrong behaviour instead of a
// compile error.
//
// InvoiceStatusOverdue is never persisted: it is derived at read time by
// EffectiveStatus — a later milestone may add a background worker that
// starts persisting it (see EffectiveStatus's own comment for how that
// stays compatible). InvoiceStatusCancelled is persisted by Cancel, the
// one way an invoice leaves the active lifecycle: invoices are never
// deleted, so a mistaken or abandoned invoice is cancelled instead and
// stays in the system as part of the organisation's record.
const (
	InvoiceStatusDraft     = "draft"
	InvoiceStatusSent      = "sent"
	InvoiceStatusPaid      = "paid"
	InvoiceStatusOverdue   = "overdue"
	InvoiceStatusCancelled = "cancelled"
)

// ErrInvoiceAlreadySent is returned by MarkSent when the invoice's
// persisted status is anything other than Draft — including an
// already-Sent or a Paid invoice. It doesn't distinguish which non-Draft
// state the invoice was in; the caller (InvoiceService.Send) doesn't need
// to, and the HTTP layer maps this to a single 409 either way.
var ErrInvoiceAlreadySent = errors.New("invoice has already been sent")

// ErrInvoiceCannotAcceptPayment is returned when a payment is attempted
// against an invoice whose persisted status isn't Sent — a Draft invoice
// (nothing has been finalised yet to pay) or a Paid invoice (nothing
// outstanding by lifecycle rule, not merely by arithmetic coincidence;
// see CanAcceptPayment).
var ErrInvoiceCannotAcceptPayment = errors.New("invoice cannot accept payment in its current status")

// ErrInvoiceAlreadyCancelled is returned by Cancel (and by Send) for an
// invoice that is already Cancelled.
var ErrInvoiceAlreadyCancelled = errors.New("invoice has already been cancelled")

// ErrInvoiceCannotBeCancelled is returned by Cancel for an invoice that
// money has already been received against — a Paid invoice, or a Sent
// one with any payment recorded. Cancelling either would leave recorded
// payments attached to an invoice that no longer asks for them; that
// needs a refund/credit note, not a status change.
var ErrInvoiceCannotBeCancelled = errors.New("invoice has payments recorded against it and cannot be cancelled")

// Notes is a pointer because that column is nullable in the invoices
// table; every other NOT NULL field is a plain value. DeletedAt and
// SentAt are pointers and stay nil until the invoice is soft-deleted or
// sent, respectively.
//
// SentAt (Milestone 5) is set exactly once, by MarkSent, at the same
// moment Status becomes InvoiceStatusSent — see MarkSent's comment for
// why the two are never written independently. A Draft invoice always has
// SentAt == nil.
//
// VATTotal (not VatTotal) matches Go's convention of keeping acronyms
// upper-cased, and matches the field name used elsewhere in this
// milestone's domain/DTO naming.
// SellerName/SellerEmail/.../CustomerCountry/Currency (Milestone 7 Part
// 2) are the immutable party snapshot, captured exactly once by MarkSent
// at the Draft -> Sent transition and never rewritten afterward — see
// InvoicePartySnapshot for what each field means and where it comes
// from. Every one of them is nil on a Draft invoice, and nil on any
// invoice Sent before this feature existed (the migration adds these as
// nullable columns specifically to accommodate that history honestly
// rather than fabricating it). SellerName, CustomerName and Currency are
// business-required for any invoice actually finalised through Send, but
// remain pointers at this struct level because the column itself must
// stay nullable for the reasons above — MarkSent is what enforces the
// requirement, not the Go type.
type Invoice struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	CustomerID     uuid.UUID
	InvoiceNumber  string
	IssueDate      time.Time
	DueDate        time.Time
	Subtotal       int64
	VATTotal       int64
	Total          int64
	Status         string // persisted lifecycle state: one of the InvoiceStatus* constants above
	SentAt         *time.Time
	Notes          *string

	// VATRegistered is whether the organisation was VAT registered when
	// this invoice was created — captured once by InvoiceService.Create
	// and never rewritten. It, not the organisation's current status,
	// decides whether VAT figures and the seller's VAT number are shown
	// for this invoice, so an issued invoice never changes appearance.
	VATRegistered bool

	SellerName       *string
	SellerEmail      *string
	SellerPhone      *string
	SellerWebsite    *string
	SellerAddress    *string
	SellerCity       *string
	SellerState      *string
	SellerPostalCode *string
	SellerCountry    *string
	SellerTaxID      *string

	CustomerName        *string
	CustomerCompanyName *string
	CustomerEmail       *string
	CustomerPhone       *string
	CustomerTaxID       *string
	CustomerAddress     *string
	CustomerCity        *string
	CustomerState       *string
	CustomerPostalCode  *string
	CustomerCountry     *string

	Currency *string

	// CancelledAt is set exactly once, by Cancel, at the same moment
	// Status becomes InvoiceStatusCancelled — nil for every other status.
	CancelledAt *time.Time

	// CustomerDisplayName is read-only display data, never written: the
	// snapshotted customer company name/name for an issued invoice, or
	// the customer record's current one for a Draft (or an invoice sent
	// before snapshots existed). It deliberately still resolves for a
	// soft-deleted customer, so historical invoices keep showing who they
	// were for. Populated by GetByID and List only.
	CustomerDisplayName string

	// RenderedTemplateSnapshot (Phase 4 of custom invoice layouts) is the
	// JSON-encoded TemplateSnapshot captured by MarkSent, alongside the
	// party snapshot above — nil for a Draft invoice, and nil for any
	// invoice sent before this feature existed. See migration 000021 and
	// TemplateSnapshot's own doc comments for the full reasoning.
	RenderedTemplateSnapshot json.RawMessage

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func (i *Invoice) TableName() string {
	return "invoices"
}

// MarkSent applies the Draft -> Sent transition to the in-memory Invoice:
// it requires the current persisted Status to be Draft, and requires
// snapshot to be valid (see InvoicePartySnapshot.Validate) — a Draft
// invoice can only become Sent together with its immutable party
// snapshot, never without one. On success it sets Status to Sent, SentAt
// to sentAt, copies every snapshot field onto the invoice, and marshals
// templateSnapshot into RenderedTemplateSnapshot (Phase 4 of custom
// invoice layouts — see TemplateSnapshot's own doc comment). Every
// non-Draft status — already Sent, Paid, or (defensively) anything else —
// is rejected with ErrInvoiceAlreadySent before snapshot is even
// examined, which is what makes a repeated Send attempt provably never
// recapture or overwrite an already-captured snapshot (Milestone 7 Part
// 2): the guard fails first, so none of the assignments below ever run.
// This method never partially applies the transition; it either changes
// every field together or changes nothing.
//
// This only mutates the Go value. InvoiceService.Send is what persists
// the result — see that method's comment for why every field is written
// together in one repository call, under the same lock this check runs
// against.
//
// No generic state-machine abstraction: with exactly one guarded
// transition, an explicit method reads more clearly than a table of
// transitions would.
func (i *Invoice) MarkSent(sentAt time.Time, snapshot InvoicePartySnapshot, templateSnapshot TemplateSnapshot) error {
	if i.Status != InvoiceStatusDraft {
		return ErrInvoiceAlreadySent
	}

	if err := i.applySnapshots(snapshot, templateSnapshot); err != nil {
		return err
	}

	i.Status = InvoiceStatusSent
	i.SentAt = &sentAt

	return nil
}

// applySnapshots validates snapshot and copies it, together with the
// marshalled templateSnapshot, onto the invoice — the shared step of
// MarkSent and of Cancel for a Draft. It changes nothing on a validation
// failure.
func (i *Invoice) applySnapshots(snapshot InvoicePartySnapshot, templateSnapshot TemplateSnapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}

	marshaledTemplateSnapshot, err := json.Marshal(templateSnapshot)
	if err != nil {
		return fmt.Errorf("marshal template snapshot: %w", err)
	}

	sellerName := snapshot.SellerName
	i.SellerName = &sellerName
	i.SellerEmail = snapshot.SellerEmail
	i.SellerPhone = snapshot.SellerPhone
	i.SellerWebsite = snapshot.SellerWebsite
	i.SellerAddress = snapshot.SellerAddress
	i.SellerCity = snapshot.SellerCity
	i.SellerState = snapshot.SellerState
	i.SellerPostalCode = snapshot.SellerPostalCode
	i.SellerCountry = snapshot.SellerCountry
	i.SellerTaxID = snapshot.SellerTaxID

	customerName := snapshot.CustomerName
	i.CustomerName = &customerName
	i.CustomerCompanyName = snapshot.CustomerCompanyName
	i.CustomerEmail = snapshot.CustomerEmail
	i.CustomerPhone = snapshot.CustomerPhone
	i.CustomerTaxID = snapshot.CustomerTaxID
	i.CustomerAddress = snapshot.CustomerAddress
	i.CustomerCity = snapshot.CustomerCity
	i.CustomerState = snapshot.CustomerState
	i.CustomerPostalCode = snapshot.CustomerPostalCode
	i.CustomerCountry = snapshot.CustomerCountry

	currency := snapshot.Currency
	i.Currency = &currency

	i.RenderedTemplateSnapshot = marshaledTemplateSnapshot

	return nil
}

// CheckCancellable reports whether the invoice may be cancelled, given
// the total already paid against it: Draft, or Sent (including
// effectively Overdue) with nothing paid. Anything paid — fully or in
// part — is ErrInvoiceCannotBeCancelled.
func (i *Invoice) CheckCancellable(amountPaid int64) error {
	switch i.Status {
	case InvoiceStatusCancelled:
		return ErrInvoiceAlreadyCancelled
	case InvoiceStatusDraft, InvoiceStatusSent:
		if amountPaid > 0 {
			return ErrInvoiceCannotBeCancelled
		}
		return nil
	default:
		return ErrInvoiceCannotBeCancelled
	}
}

// Cancel applies the -> Cancelled transition to the in-memory Invoice
// after re-checking CheckCancellable. A Draft has no snapshot yet, so
// cancelling one captures snapshot/templateSnapshot exactly as MarkSent
// would: every Cancelled invoice is then a self-contained historical
// record, read from its own snapshot (currency, PDF, customer name) like
// any other non-Draft invoice — which is also what lets its customer be
// deleted later without breaking it. For an already-Sent invoice the
// snapshot it captured when sent is kept, and both arguments are
// ignored. SentAt is never set here: a cancelled Draft was never sent.
func (i *Invoice) Cancel(cancelledAt time.Time, amountPaid int64, snapshot InvoicePartySnapshot, templateSnapshot TemplateSnapshot) error {
	if err := i.CheckCancellable(amountPaid); err != nil {
		return err
	}

	if i.Status == InvoiceStatusDraft {
		if err := i.applySnapshots(snapshot, templateSnapshot); err != nil {
			return err
		}
	}

	i.Status = InvoiceStatusCancelled
	i.CancelledAt = &cancelledAt

	return nil
}

// CanAcceptPayment reports whether a payment may currently be recorded
// against this invoice, based on persisted Status alone — deliberately
// not EffectiveStatus. In Milestone 5 an invoice displayed to a client as
// "overdue" is still persisted as Sent, and it must remain payable; using
// EffectiveStatus here would make payment eligibility depend on the
// wall-clock moment the check runs, which is not the intended rule. Once
// a later milestone starts persisting Overdue for real, this should
// extend to include it explicitly (Status == Sent || Status == Overdue),
// not by switching to EffectiveStatus.
func (i *Invoice) CanAcceptPayment() bool {
	return i.Status == InvoiceStatusSent
}

// AmountOutstanding is what is still owed on the invoice given amountPaid:
// Total - amountPaid, except that a Cancelled invoice owes nothing.
func (i *Invoice) AmountOutstanding(amountPaid int64) int64 {
	if i.Status == InvoiceStatusCancelled {
		return 0
	}

	return i.Total - amountPaid
}

// EffectiveStatus derives the status an API response should show, from
// persisted Status and the current time — it is the sole replacement for
// the old, incorrect IsOverdue method (which considered a Draft invoice
// overdue whenever its due date had passed, since it only tested
// Status != Paid). It never mutates the Invoice and never reads the
// system clock itself — now is supplied by the caller (the HTTP/DTO
// mapping layer), keeping this pure and deterministically testable.
//
// Only a persisted Sent invoice is ever re-derived as Overdue; Draft and
// Paid are returned unchanged regardless of DueDate. A persisted Overdue
// value — which Milestone 5 never writes, but a future milestone's
// background worker eventually will — is passed through as-is rather
// than re-evaluated against DueDate, so this function stays correct
// without modification once that worker exists.
//
// Due-date semantics: IssueDate and DueDate represent calendar dates, not
// precise instants (they come from DATE columns, scanned as UTC
// midnight) — the invoice becomes overdue only on the calendar day AFTER
// DueDate, never from the first moment of DueDate itself. Comparing full
// timestamps (now.After(DueDate)) would make an invoice appear overdue
// from midnight on its own due date, which is wrong; see isPastDueDate.
func (i *Invoice) EffectiveStatus(now time.Time) string {
	if i.Status != InvoiceStatusSent {
		return i.Status
	}

	if isPastDueDate(now, i.DueDate) {
		return InvoiceStatusOverdue
	}

	return InvoiceStatusSent
}

// isPastDueDate reports whether now's calendar date (in UTC) is strictly
// after dueDate's calendar date. dueDate (from a DATE column) is always
// already UTC midnight; see UTCDate for how now is truncated to the same
// form before comparing.
func isPastDueDate(now, dueDate time.Time) bool {
	return UTCDate(now).After(dueDate)
}

// UTCDate truncates now to its UTC calendar date at midnight — the exact
// "today" boundary isPastDueDate (and therefore EffectiveStatus) uses to
// decide whether a persisted Sent invoice is effectively Overdue.
// Exported (Milestone 8 Part 3) so InvoiceService.List can compute the
// same boundary once and pass it into both its SQL predicate and every
// row's EffectiveStatus call — one definition of "today" shared by SQL
// and Go, rather than two that could quietly drift apart.
func UTCDate(now time.Time) time.Time {
	nowUTC := now.UTC()
	return time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
}
