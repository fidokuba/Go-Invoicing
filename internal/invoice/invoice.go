package invoice

import (
	"errors"
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
// InvoiceStatusOverdue and InvoiceStatusCancelled are Milestone 5's
// reserved-but-unused values: nothing in this milestone ever persists
// either. Overdue is derived at read time by EffectiveStatus, never
// written to the database — a later milestone may add a background
// worker that starts persisting it (see EffectiveStatus's own comment for
// how that stays compatible). Cancelled has no feature behind it at all
// yet; it exists only so a future milestone doesn't need to touch this
// const block to add one.
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
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

func (i *Invoice) TableName() string {
	return "invoices"
}

// MarkSent applies the Draft -> Sent transition to the in-memory Invoice:
// it requires the current persisted Status to be Draft, and on success
// sets Status to Sent and SentAt to sentAt. Every non-Draft status —
// already Sent, Paid, or (defensively) anything else — is rejected with
// ErrInvoiceAlreadySent; this method never partially applies the
// transition; it either changes both fields together or changes neither.
//
// This only mutates the Go value. InvoiceService.Send is what persists
// the result — see that method's comment for why the two fields are
// written together in one repository call, under the same lock this
// check runs against.
//
// No generic state-machine abstraction: with exactly one guarded
// transition, an explicit method reads more clearly than a table of
// transitions would.
func (i *Invoice) MarkSent(sentAt time.Time) error {
	if i.Status != InvoiceStatusDraft {
		return ErrInvoiceAlreadySent
	}

	i.Status = InvoiceStatusSent
	i.SentAt = &sentAt

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
// after dueDate's calendar date. now is truncated to UTC midnight before
// comparing — regardless of what time of day the check happens to run,
// only which calendar day now falls on matters, and dueDate (from a DATE
// column) is always already UTC midnight.
func isPastDueDate(now, dueDate time.Time) bool {
	nowUTC := now.UTC()
	today := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)

	return today.After(dueDate)
}
