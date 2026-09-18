package invoice

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestInvoiceService_Send_PersistsStatusAndSentAt proves a successful
// Send actually writes status=sent and sent_at to PostgreSQL, not just
// returns them in memory — reading back through a fresh repository call
// rather than trusting the in-memory return value, the same pattern used
// throughout this package's other "persisted" tests.
func TestInvoiceService_Send_PersistsStatusAndSentAt(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	inv, err := service.Send(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if inv.SentAt == nil {
		t.Fatal("expected SentAt to be populated on the returned invoice")
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	persisted, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.Status != InvoiceStatusSent {
		t.Errorf("expected persisted status %q, got %q", InvoiceStatusSent, persisted.Status)
	}

	if persisted.SentAt == nil {
		t.Fatal("expected persisted SentAt to be populated")
	}

	if !persisted.SentAt.Equal(*inv.SentAt) {
		t.Errorf("expected persisted SentAt %v, got %v", *inv.SentAt, *persisted.SentAt)
	}
}

// TestInvoiceService_Send_RepeatedSendLeavesStateUnchanged proves a
// rejected repeat Send doesn't alter the invoice's already-persisted
// status or sent_at in any way.
func TestInvoiceService_Send_RepeatedSendLeavesStateUnchanged(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	first, err := service.Send(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}

	_, err = service.Send(ctx, organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent on the repeat send, got %v", err)
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	persisted, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.Status != InvoiceStatusSent {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusSent, persisted.Status)
	}

	if !persisted.SentAt.Equal(*first.SentAt) {
		t.Errorf("expected SentAt to remain %v, got %v", *first.SentAt, *persisted.SentAt)
	}
}

// TestInvoiceService_Send_ConcurrentSend_ExactlyOneSucceeds is the
// concurrency proof: two goroutines call Send on the same Draft invoice
// at the same time. Exactly one must succeed and the other must observe
// the now-Sent status after acquiring the row lock, returning
// ErrInvoiceAlreadySent — never two successes, and never a corrupted or
// partially-applied status/sent_at pair. Mirrors this package's existing
// concurrency-test style (e.g.
// TestInvoiceService_CreatePayment_ConcurrentPaymentsCannotOverpay).
func TestInvoiceService_Send_ConcurrentSend_ExactlyOneSucceeds(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	const attempts = 2

	var wg sync.WaitGroup
	errs := make([]error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := service.Send(ctx, organisationID, invoiceID)
			errs[i] = err
		}(i)
	}

	wg.Wait()

	successCount := 0
	conflictCount := 0

	for i, err := range errs {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrInvoiceAlreadySent):
			conflictCount++
		default:
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful send, got %d", successCount)
	}

	if conflictCount != 1 {
		t.Errorf("expected exactly 1 send rejected via ErrInvoiceAlreadySent, got %d", conflictCount)
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	persisted, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.Status != InvoiceStatusSent {
		t.Errorf("expected final status %q, got %q", InvoiceStatusSent, persisted.Status)
	}

	if persisted.SentAt == nil {
		t.Error("expected SentAt to be populated exactly once")
	}
}

// TestInvoiceService_CreatePayment_DraftAttemptCreatesNoPaymentRows
// proves, against real PostgreSQL, that an attempt to pay a Draft
// invoice is rejected before ever reaching the payments table — no
// partial or orphan payment row is created.
func TestInvoiceService_CreatePayment_DraftAttemptCreatesNoPaymentRows(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	_, _, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
		Amount:        5000,
		PaymentMethod: "cash",
	})
	if !errors.Is(err, ErrInvoiceCannotAcceptPayment) {
		t.Fatalf("expected ErrInvoiceCannotAcceptPayment, got %v", err)
	}

	var paymentCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM payments WHERE invoice_id = $1", invoiceID).Scan(&paymentCount); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if paymentCount != 0 {
		t.Errorf("expected 0 payment rows after a rejected Draft payment attempt, got %d", paymentCount)
	}
}

// TestInvoiceService_SendThenPaymentRace_SerializesSafely proves the
// Send/payment race Milestone 5 asks about is already safely serialized
// by the shared FOR UPDATE lock, without any new synchronization
// infrastructure: a goroutine sending the invoice and a goroutine
// attempting to pay it race for the same row lock. Whichever runs first
// determines the other's outcome deterministically — either the payment
// sees a Sent invoice and succeeds, or it sees a still-Draft invoice and
// is rejected — but the two can never observe inconsistent intermediate
// state, and exactly one of Send/payment-success combinations is
// possible per run. This test doesn't assert which one wins (that's
// timing-dependent and not the guarantee being proven); it asserts the
// end state is always one of the two valid outcomes, never a third,
// corrupted one.
func TestInvoiceService_SendThenPaymentRace_SerializesSafely(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	var wg sync.WaitGroup
	var sendErr, paymentErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, sendErr = service.Send(ctx, organisationID, invoiceID)
	}()
	go func() {
		defer wg.Done()
		_, _, paymentErr = service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
			Amount:        10000,
			PaymentMethod: "cash",
		})
	}()
	wg.Wait()

	// Send must always succeed — nothing else can ever change a Draft
	// invoice's status out from under it, regardless of ordering.
	if sendErr != nil {
		t.Fatalf("expected Send to always succeed regardless of race outcome, got %v", sendErr)
	}

	// The payment either won the race against a still-Draft invoice
	// (rejected) or ran after Send committed (accepted) — both are valid,
	// mutually exclusive outcomes; anything else is a defect.
	if paymentErr != nil && !errors.Is(paymentErr, ErrInvoiceCannotAcceptPayment) {
		t.Fatalf("expected either a successful payment or ErrInvoiceCannotAcceptPayment, got %v", paymentErr)
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	persisted, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	paymentRepository := NewPostgresPaymentRepository(db)
	payments, err := paymentRepository.GetByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}

	if paymentErr == nil {
		// The payment succeeded: it must have paid the invoice in full.
		if persisted.Status != InvoiceStatusPaid {
			t.Errorf("expected status %q after a successful full payment, got %q", InvoiceStatusPaid, persisted.Status)
		}
		if len(payments) != 1 {
			t.Errorf("expected exactly 1 payment row, got %d", len(payments))
		}
	} else {
		// The payment was rejected: the invoice must be Sent (from the
		// concurrent Send) with no payment recorded at all.
		if persisted.Status != InvoiceStatusSent {
			t.Errorf("expected status %q after a rejected payment, got %q", InvoiceStatusSent, persisted.Status)
		}
		if len(payments) != 0 {
			t.Errorf("expected 0 payment rows after a rejected payment, got %d", len(payments))
		}
	}
}
