package invoice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func validPaymentRequest() CreatePaymentRequest {
	return CreatePaymentRequest{
		Amount:        4000,
		PaymentMethod: "cash",
	}
}

// --- Lifecycle eligibility (Milestone 5) ---

// TestInvoiceService_CreatePayment_DraftRejected proves the lifecycle gap
// the Milestone 5 investigation found — a Draft invoice could previously
// be paid directly — is closed: CreatePayment now checks
// Invoice.CanAcceptPayment before any payment math runs.
func TestInvoiceService_CreatePayment_DraftRejected(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if !errors.Is(err, ErrInvoiceCannotAcceptPayment) {
		t.Fatalf("expected ErrInvoiceCannotAcceptPayment, got %v", err)
	}
}

// TestInvoiceService_CreatePayment_DraftRejectionWritesNoPayment proves
// the rejection happens before the payment repository is ever reached —
// not merely that the right error is returned while something is still
// written.
func TestInvoiceService_CreatePayment_DraftRejectionWritesNoPayment(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	if _, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest()); err == nil {
		t.Fatal("expected an error")
	}

	if len(f.paymentRepository.payments[invoiceID]) != 0 {
		t.Errorf("expected no payment to have been created, got %d", len(f.paymentRepository.payments[invoiceID]))
	}
}

func TestInvoiceService_CreatePayment_PaidRejectedExplicitly(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusPaid)

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if !errors.Is(err, ErrInvoiceCannotAcceptPayment) {
		t.Fatalf("expected ErrInvoiceCannotAcceptPayment, got %v", err)
	}
}

// --- Validation ---

func TestInvoiceService_CreatePayment_Success(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	payment, inv, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if payment.ID == uuid.Nil {
		t.Error("expected a generated payment ID")
	}

	if payment.InvoiceID != invoiceID {
		t.Errorf("expected invoice ID %v, got %v", invoiceID, payment.InvoiceID)
	}

	if payment.Amount != 4000 {
		t.Errorf("expected amount 4000, got %d", payment.Amount)
	}

	if inv.ID != invoiceID {
		t.Errorf("expected returned invoice ID %v, got %v", invoiceID, inv.ID)
	}
}

func TestInvoiceService_CreatePayment_ZeroAmount(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := validPaymentRequest()
	request.Amount = 0

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if !errors.Is(err, ErrPaymentAmountInvalid) {
		t.Fatalf("expected ErrPaymentAmountInvalid, got %v", err)
	}
}

func TestInvoiceService_CreatePayment_NegativeAmount(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := validPaymentRequest()
	request.Amount = -500

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if !errors.Is(err, ErrPaymentAmountInvalid) {
		t.Fatalf("expected ErrPaymentAmountInvalid, got %v", err)
	}
}

// --- Status transitions ---

func TestInvoiceService_CreatePayment_PartialPayment_LeavesStatusUnchanged(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := validPaymentRequest()
	request.Amount = 4000 // less than the 10000 total: partial

	_, inv, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if inv.Status != InvoiceStatusSent {
		t.Errorf("expected status to remain %q after a partial payment, got %q", InvoiceStatusSent, inv.Status)
	}
}

func TestInvoiceService_CreatePayment_FullPayment_SetsStatusPaid(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := validPaymentRequest()
	request.Amount = 10000 // exactly the outstanding balance: full payment

	_, inv, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if inv.Status != InvoiceStatusPaid {
		t.Errorf("expected status %q after a full payment, got %q", InvoiceStatusPaid, inv.Status)
	}
}

func TestInvoiceService_CreatePayment_SecondPaymentCompletesInvoice(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	ctx := context.Background()

	first := validPaymentRequest()
	first.Amount = 6000
	if _, inv, err := f.service.CreatePayment(ctx, f.organisationID, invoiceID, first); err != nil {
		t.Fatalf("create first payment: %v", err)
	} else if inv.Status != InvoiceStatusSent {
		t.Fatalf("expected status to remain %q after the first (partial) payment, got %q", InvoiceStatusSent, inv.Status)
	}

	second := validPaymentRequest()
	second.Amount = 4000 // exhausts the remaining 4000 outstanding
	_, inv, err := f.service.CreatePayment(ctx, f.organisationID, invoiceID, second)
	if err != nil {
		t.Fatalf("create second payment: %v", err)
	}

	if inv.Status != InvoiceStatusPaid {
		t.Errorf("expected status %q after the second payment exhausts the balance, got %q", InvoiceStatusPaid, inv.Status)
	}
}

// --- Overpayment ---

func TestInvoiceService_CreatePayment_Overpayment_Rejected(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := validPaymentRequest()
	request.Amount = 10001 // 1 minor unit more than the outstanding balance

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if !errors.Is(err, ErrPaymentExceedsOutstanding) {
		t.Fatalf("expected ErrPaymentExceedsOutstanding, got %v", err)
	}
}

// TestInvoiceService_CreatePayment_AgainstAlreadyPaidInvoice_Rejected
// proves a Paid invoice is now rejected by the Milestone 5 lifecycle
// check (Invoice.CanAcceptPayment) itself, not merely as a coincidence of
// its outstanding balance already being 0 — the explicit check runs
// before the outstanding-balance math is ever reached.
func TestInvoiceService_CreatePayment_AgainstAlreadyPaidInvoice_Rejected(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusPaid)
	f.paymentRepository.payments[invoiceID] = []*Payment{
		{ID: uuid.New(), InvoiceID: invoiceID, Amount: 10000},
	}

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if !errors.Is(err, ErrInvoiceCannotAcceptPayment) {
		t.Fatalf("expected ErrInvoiceCannotAcceptPayment for a payment against an already-paid invoice, got %v", err)
	}
}

// --- Invoice existence / organisation scoping ---

func TestInvoiceService_CreatePayment_InvoiceNotFound(t *testing.T) {
	f := newTestFixture()

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, uuid.New(), validPaymentRequest())
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound, got %v", err)
	}
}

func TestInvoiceService_CreatePayment_WrongOrganisation_Rejected(t *testing.T) {
	// An invoice that exists, but under a different organisation, must be
	// treated as not found — a caller cannot pay against another
	// organisation's invoice by knowing its UUID.
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	_, _, err := f.service.CreatePayment(context.Background(), uuid.New(), invoiceID, validPaymentRequest())
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-organisation payment, got %v", err)
	}

	// The rejection must happen at GetForUpdate, before the payment
	// repository is ever reached — not merely return the right error
	// while still writing something.
	if len(f.paymentRepository.payments[invoiceID]) != 0 {
		t.Errorf("expected no payment to have been created for a cross-organisation attempt, got %d", len(f.paymentRepository.payments[invoiceID]))
	}
}

// --- Transaction orchestration (fakes; real atomicity is proven against
// PostgreSQL in invoice_transaction_test.go) ---

func TestInvoiceService_CreatePayment_CommitsOnSuccess(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if !f.tx.committed {
		t.Error("expected the transaction to be committed")
	}

	if f.tx.rolledBack {
		t.Error("expected the transaction not to be rolled back")
	}
}

func TestInvoiceService_CreatePayment_RollsBackOnCreateFailure(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	f.paymentRepository.createErr = errors.New("connection reset by peer")

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestInvoiceService_CreatePayment_RollsBackOnStatusUpdateFailure(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	f.repository.updateStatusErr = errors.New("connection reset by peer")

	request := validPaymentRequest()
	request.Amount = 10000 // a full payment, so the status update is attempted

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}

	// The fake repositories have no real rollback semantics of their own
	// (they don't undo a prior successful Create when a later step in the
	// same transaction fails) — that guarantee, that the payment insert
	// really is undone along with the failed status update, is proven
	// against real PostgreSQL instead, in invoice_transaction_test.go.
	// Here we only check the service's own control flow: it must roll
	// back and report failure rather than committing a half-applied
	// change.
}

func TestInvoiceService_CreatePayment_BeginError(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	f.service.txBeginner = &fakeTxBeginner{beginErr: errors.New("pool exhausted")}

	_, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if err == nil {
		t.Fatal("expected an error when Begin fails, got nil")
	}
}
