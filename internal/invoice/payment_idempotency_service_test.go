package invoice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Milestone 13 Part 1: InvoiceService.CreatePayment's idempotency
// orchestration, against fakes. The real transactional, concurrency and
// constraint guarantees are proven against PostgreSQL in
// payment_idempotency_transaction_test.go.

func TestInvoiceService_CreatePayment_InvalidKeyRejectedBeforeTransaction(t *testing.T) {
	for _, key := range []string{"", "too-short", "has a space in it!"} {
		f := newTestFixture()
		invoiceID := f.addInvoice(10000, InvoiceStatusSent)
		// Any Begin would fail loudly with this error instead.
		f.service.txBeginner = &fakeTxBeginner{beginErr: errors.New("begin must not be reached")}

		request := validPaymentRequest()
		request.IdempotencyKey = key

		_, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
		if !errors.Is(err, ErrPaymentIdempotencyKeyInvalid) {
			t.Fatalf("key %q: expected ErrPaymentIdempotencyKeyInvalid, got %v", key, err)
		}
	}
}

func TestInvoiceService_CreatePayment_ReplayReturnsOriginalWithoutWriting(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	request := validPaymentRequest()

	first, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	if first.Replayed {
		t.Fatal("expected the first attempt not to be a replay")
	}

	f.tx.committed, f.tx.rolledBack = false, false

	second, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if !second.Replayed {
		t.Error("expected the retry to be reported as a replay")
	}
	if second.Payment.ID != first.Payment.ID {
		t.Errorf("expected the replay to return payment %s, got %s", first.Payment.ID, second.Payment.ID)
	}
	if second.Invoice != nil {
		t.Error("expected a replay not to return an invoice")
	}
	if got := len(f.paymentRepository.payments[invoiceID]); got != 1 {
		t.Errorf("expected exactly 1 payment, got %d", got)
	}
	if f.tx.committed {
		t.Error("expected a replay not to commit anything")
	}
	if !f.tx.rolledBack {
		t.Error("expected a replay's transaction to be rolled back (releasing the invoice lock)")
	}
}

// The ordering requirement: the key lookup must precede CanAcceptPayment,
// so retrying the payment that settled the invoice replays it instead of
// failing because the invoice is now Paid.
func TestInvoiceService_CreatePayment_FullPaymentRetryReplaysNotLifecycleConflict(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	request := validPaymentRequest()
	request.Amount = 10000

	first, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	if first.Invoice.Status != InvoiceStatusPaid {
		t.Fatalf("expected the full payment to mark the invoice Paid, got %q", first.Invoice.Status)
	}

	second, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("expected the retry to replay, got %v", err)
	}
	if !second.Replayed || second.Payment.ID != first.Payment.ID {
		t.Errorf("expected a replay of %s, got replayed=%v id=%s", first.Payment.ID, second.Replayed, second.Payment.ID)
	}
}

func TestInvoiceService_CreatePayment_SameKeyDifferentPayloadConflicts(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	request := validPaymentRequest()

	if _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request); err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	f.tx.committed, f.tx.rolledBack = false, false
	request.Amount++

	_, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request)
	if !errors.Is(err, ErrPaymentIdempotencyKeyReused) {
		t.Fatalf("expected ErrPaymentIdempotencyKeyReused, got %v", err)
	}
	if got := len(f.paymentRepository.payments[invoiceID]); got != 1 {
		t.Errorf("expected exactly 1 payment, got %d", got)
	}
	if f.tx.committed {
		t.Error("expected a conflict not to commit anything")
	}
}

func TestInvoiceService_CreatePayment_KeyLookupFailureRollsBack(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	f.paymentRepository.getByIdempotencyKeyErr = errors.New("connection reset by peer")

	_, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, validPaymentRequest())
	if err == nil || errors.Is(err, ErrPaymentIdempotencyKeyReused) {
		t.Fatalf("expected an internal lookup error, got %v", err)
	}
	if len(f.paymentRepository.payments[invoiceID]) != 0 {
		t.Error("expected no payment to be written")
	}
	if !f.tx.rolledBack || f.tx.committed {
		t.Error("expected the transaction to be rolled back, not committed")
	}
}

// A cross-tenant request fails at GetForUpdate — before the key is ever
// looked up — so it can never observe another tenant's key.
func TestInvoiceService_CreatePayment_CrossTenantNeverReachesKeyLookup(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	request := validPaymentRequest()

	if _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, request); err != nil {
		t.Fatalf("owner's payment: %v", err)
	}

	// Any key lookup would now fail loudly instead of returning a result.
	f.paymentRepository.getByIdempotencyKeyErr = errors.New("key lookup must not be reached")

	_, err := f.service.CreatePayment(context.Background(), uuid.New(), invoiceID, request)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound, got %v", err)
	}
}
