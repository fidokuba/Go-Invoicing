package invoice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/product"
)

// createTestInvoiceWithTotal inserts an invoice with a specific Total and
// Status directly via the invoice repository (not via InvoiceService, to
// avoid pulling in the whole invoice-creation flow for tests that only
// care about payments against an already-existing invoice), and registers
// cleanup for both it and any payments recorded against it.
func createTestInvoiceWithTotal(
	t *testing.T,
	db *pgxpool.Pool,
	organisationID, customerID uuid.UUID,
	total int64,
	status string,
) uuid.UUID {
	t.Helper()

	repository := NewPostgresInvoiceRepository(db)
	inv := &Invoice{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		CustomerID:     customerID,
		InvoiceNumber:  "INV-TEST-" + uuid.New().String(),
		IssueDate:      time.Now().UTC().Truncate(24 * time.Hour),
		DueDate:        time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 30),
		Total:          total,
		Status:         status,
	}

	if err := repository.Create(context.Background(), inv); err != nil {
		t.Fatalf("create test invoice: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM payments WHERE invoice_id = $1", inv.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", inv.ID)
	})

	return inv.ID
}

// newPaymentTestService wires a real InvoiceService against real
// PostgreSQL repositories — the same construction app.go does — for tests
// that need to prove behaviour fakes can't (real atomicity, real locking,
// real concurrency).
func newPaymentTestService(db *pgxpool.Pool) *InvoiceService {
	return NewInvoiceService(
		NewPostgresInvoiceRepository(db),
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresAddressRepository(db),
		admin.NewPostgresSettingsRepository(db),
		NewPostgresPaymentRepository(db),
		db,
	)
}

// TestInvoiceService_CreatePayment_Persisted proves a successful payment
// is actually written to PostgreSQL, not just returned in memory.
func TestInvoiceService_CreatePayment_Persisted(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusSent)

	service := newPaymentTestService(db)

	result, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
		IdempotencyKey: newTestIdempotencyKey(),
		Amount:         4000,
		PaymentMethod:  "cash",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	payment := result.Payment

	paymentRepository := NewPostgresPaymentRepository(db)

	persisted, err := paymentRepository.GetByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}

	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted payment, got %d", len(persisted))
	}

	if persisted[0].ID != payment.ID {
		t.Errorf("expected persisted payment ID %v, got %v", payment.ID, persisted[0].ID)
	}

	if persisted[0].Amount != 4000 {
		t.Errorf("expected persisted amount 4000, got %d", persisted[0].Amount)
	}
}

// TestInvoiceService_CreatePayment_MultiplePaymentsAccumulate proves
// several payments against the same invoice sum correctly.
func TestInvoiceService_CreatePayment_MultiplePaymentsAccumulate(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusSent)

	service := newPaymentTestService(db)

	amounts := []int64{2000, 3000, 4000} // sums to 9000, deliberately under the 10000 total
	for _, amount := range amounts {
		if _, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
			IdempotencyKey: newTestIdempotencyKey(),
			Amount:         amount,
			PaymentMethod:  "cash",
		}); err != nil {
			t.Fatalf("create payment of %d: %v", amount, err)
		}
	}

	paymentRepository := NewPostgresPaymentRepository(db)

	total, err := paymentRepository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get total paid: %v", err)
	}

	wantTotal := int64(2000 + 3000 + 4000)
	if total != wantTotal {
		t.Errorf("expected total paid %d, got %d", wantTotal, total)
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	inv, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if inv.Status != InvoiceStatusSent {
		t.Errorf("expected status to remain %q since the invoice isn't fully paid, got %q", InvoiceStatusSent, inv.Status)
	}
}

// TestInvoiceService_CreatePayment_FullPayment_UpdatesStatusInDB proves the
// invoice's status is actually persisted as "paid" (InvoiceStatusPaid) —
// read back through a fresh repository call, not the in-memory return
// value — once a payment exhausts the outstanding balance.
func TestInvoiceService_CreatePayment_FullPayment_UpdatesStatusInDB(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusSent)

	service := newPaymentTestService(db)

	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
		IdempotencyKey: newTestIdempotencyKey(),
		Amount:         10000,
		PaymentMethod:  "bank_transfer",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	inv, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if inv.Status != InvoiceStatusPaid {
		t.Errorf("expected persisted status %q, got %q", InvoiceStatusPaid, inv.Status)
	}
}

// TestInvoiceService_CreatePayment_OverpaymentTransaction_CreatesNothing
// proves that a rejected (overpaying) payment attempt leaves no trace: no
// payment row, and the invoice's own state untouched. The rejection
// happens inside the transaction (after the lock is taken, once the
// outstanding balance is known) but before Create or UpdateStatus are
// ever called, so this also demonstrates that a failed payment
// transaction does not create a payment.
func TestInvoiceService_CreatePayment_OverpaymentTransaction_CreatesNothing(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusSent)

	service := newPaymentTestService(db)

	_, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
		IdempotencyKey: newTestIdempotencyKey(),
		Amount:         10001, // 1 minor unit more than the outstanding balance
		PaymentMethod:  "cash",
	})
	if !errors.Is(err, ErrPaymentExceedsOutstanding) {
		t.Fatalf("expected ErrPaymentExceedsOutstanding, got %v", err)
	}

	paymentRepository := NewPostgresPaymentRepository(db)

	payments, err := paymentRepository.GetByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}

	if len(payments) != 0 {
		t.Errorf("expected 0 payments after a rejected overpayment, got %d", len(payments))
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	inv, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if inv.Status != InvoiceStatusSent {
		t.Errorf("expected status to remain %q after a rejected payment, got %q", InvoiceStatusSent, inv.Status)
	}
}

// TestInvoiceService_CreatePayment_WrongOrganisation_LeavesNoTrace is the
// real-Postgres counterpart to the fake-based
// TestInvoiceService_CreatePayment_WrongOrganisation_Rejected: proves,
// against the real database, that an authenticated Organisation A cannot
// create a payment against an Organisation B invoice, and that the
// attempt leaves the database completely unchanged — no new payment row,
// and the invoice's own status untouched. The rejection happens at
// GetForUpdate, before the transaction ever reaches the payment
// repository or the status update, exactly as
// TestInvoiceService_CreatePayment_OverpaymentTransaction_CreatesNothing
// proves for a different rejection reason.
func TestInvoiceService_CreatePayment_WrongOrganisation_LeavesNoTrace(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationB)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationB, customerID, 10000, InvoiceStatusSent)

	service := newPaymentTestService(db)

	_, err := service.CreatePayment(ctx, organisationA, invoiceID, CreatePaymentRequest{
		IdempotencyKey: newTestIdempotencyKey(),
		Amount:         5000,
		PaymentMethod:  "cash",
	})
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-organisation payment attempt, got %v", err)
	}

	var paymentCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM payments WHERE invoice_id = $1", invoiceID).Scan(&paymentCount); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if paymentCount != 0 {
		t.Errorf("expected 0 payments after a rejected cross-organisation attempt, got %d", paymentCount)
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	inv, err := invoiceRepository.GetByID(ctx, organisationB, invoiceID)
	if err != nil {
		t.Fatalf("get invoice under its real organisation: %v", err)
	}
	if inv.Status != InvoiceStatusSent {
		t.Errorf("expected status to remain %q after the rejected cross-organisation attempt, got %q", InvoiceStatusSent, inv.Status)
	}

	// The organisation A caller must never be able to observe organisation
	// B's invoice at all, from any angle GetPayments uses either.
	payments, err := service.GetPayments(ctx, organisationA, invoiceID)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound listing payments for a cross-organisation invoice, got %v", err)
	}
	if payments != nil {
		t.Errorf("expected a nil payments slice on rejection, got %+v", payments)
	}
}

// TestInvoiceService_CreatePayment_ConcurrentPaymentsCannotOverpay is the
// concurrency proof: two goroutines each attempt to pay the invoice's
// entire £100.00 balance at the same time. Exactly one must succeed;
// the other must be rejected, regardless of which one happens to win the
// race for the invoice's row lock — the assertions below only look at
// final database state and success/failure counts, never at which
// goroutine "won".
//
// The loser's rejection reason is Milestone 5's lifecycle check
// (ErrInvoiceCannotAcceptPayment), not the older outstanding-balance
// check (ErrPaymentExceedsOutstanding): both goroutines attempt to pay
// the exact full balance, so whichever one is second to acquire the lock
// finds the invoice already Paid — by the winner's own commit — rather
// than merely fully-outstanding-but-still-Sent. A genuine
// still-Sent-but-overpaying scenario would still surface
// ErrPaymentExceedsOutstanding instead; that path is unchanged and
// already covered by the non-concurrent overpayment tests.
func TestInvoiceService_CreatePayment_ConcurrentPaymentsCannotOverpay(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusSent) // £100.00

	service := newPaymentTestService(db)

	const attempts = 2

	var wg sync.WaitGroup
	errs := make([]error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			_, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
				IdempotencyKey: newTestIdempotencyKey(),
				Amount:         10000, // each attempts to pay the full balance
				PaymentMethod:  "cash",
			})
			errs[i] = err
		}(i)
	}

	wg.Wait()

	successCount := 0
	rejectionCount := 0

	for i, err := range errs {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrInvoiceCannotAcceptPayment):
			rejectionCount++
		default:
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful payment, got %d", successCount)
	}

	if rejectionCount != 1 {
		t.Errorf("expected exactly 1 payment rejected via ErrInvoiceCannotAcceptPayment, got %d", rejectionCount)
	}

	paymentRepository := NewPostgresPaymentRepository(db)

	payments, err := paymentRepository.GetByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}

	if len(payments) != 1 {
		t.Fatalf("expected exactly 1 payment row, got %d", len(payments))
	}

	total, err := paymentRepository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get total paid: %v", err)
	}

	if total != 10000 {
		t.Errorf("expected total paid 10000 (£100.00), got %d", total)
	}

	invoiceRepository := NewPostgresInvoiceRepository(db)
	inv, err := invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if inv.Status != InvoiceStatusPaid {
		t.Errorf("expected invoice status %q, got %q", InvoiceStatusPaid, inv.Status)
	}
}
