package invoice

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createTestInvoice inserts a minimal invoice row directly via the
// invoice repository (not via InvoiceService, to avoid pulling in
// settings/transaction machinery unrelated to payment tests) and
// registers cleanup for it.
func createTestInvoice(t *testing.T, db *pgxpool.Pool, organisationID, customerID uuid.UUID) uuid.UUID {
	t.Helper()

	repository := NewPostgresInvoiceRepository(db)
	inv := &Invoice{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		CustomerID:     customerID,
		InvoiceNumber:  "INV-TEST-" + uuid.New().String(),
		IssueDate:      time.Now().UTC().Truncate(24 * time.Hour),
		DueDate:        time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 30),
		Status:         InvoiceStatusDraft,
	}

	if err := repository.Create(context.Background(), inv); err != nil {
		t.Fatalf("create test invoice: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", inv.ID)
	})

	return inv.ID
}

func TestPostgresPaymentRepository_CreateAndGetByInvoiceID(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoice(t, db, organisationID, customerID)

	repository := NewPostgresPaymentRepository(db)

	reference := "TXN-123"
	notes := "Paid by bank transfer"
	payment := &Payment{
		ID:            uuid.New(),
		InvoiceID:     invoiceID,
		Amount:        1500,
		PaymentMethod: "bank_transfer",
		PaymentDate:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Reference:     &reference,
		Notes:         &notes,
	}

	if err := repository.Create(ctx, organisationID, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM payments WHERE id = $1", payment.ID)
	})

	payments, err := repository.GetByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}

	if len(payments) != 1 {
		t.Fatalf("expected 1 payment, got %d", len(payments))
	}

	got := payments[0]

	if got.ID != payment.ID {
		t.Errorf("expected ID %v, got %v", payment.ID, got.ID)
	}

	if got.InvoiceID != invoiceID {
		t.Errorf("expected invoice ID %v, got %v", invoiceID, got.InvoiceID)
	}

	if got.Amount != 1500 {
		t.Errorf("expected amount 1500, got %d", got.Amount)
	}

	if got.PaymentMethod != "bank_transfer" {
		t.Errorf("expected payment method %q, got %q", "bank_transfer", got.PaymentMethod)
	}

	if got.PaymentDate.Format("2006-01-02") != "2026-01-15" {
		t.Errorf("expected payment date 2026-01-15, got %s", got.PaymentDate.Format("2006-01-02"))
	}

	if got.Reference == nil || *got.Reference != reference {
		t.Errorf("expected reference %q, got %v", reference, got.Reference)
	}

	if got.Notes == nil || *got.Notes != notes {
		t.Errorf("expected notes %q, got %v", notes, got.Notes)
	}
}

func TestPostgresPaymentRepository_GetByInvoiceID_Empty(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoice(t, db, organisationID, customerID)

	repository := NewPostgresPaymentRepository(db)

	payments, err := repository.GetByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}

	if len(payments) != 0 {
		t.Errorf("expected 0 payments, got %d", len(payments))
	}
}

func TestPostgresPaymentRepository_GetByInvoiceID_OrderedByDateThenCreatedAt(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoice(t, db, organisationID, customerID)

	repository := NewPostgresPaymentRepository(db)

	// Inserted out of date order; GetByInvoiceID must still return them
	// in ascending payment_date order regardless of insert/creation order.
	dates := []time.Time{
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	var ids []uuid.UUID
	for _, d := range dates {
		p := &Payment{
			ID:            uuid.New(),
			InvoiceID:     invoiceID,
			Amount:        100,
			PaymentMethod: "cash",
			PaymentDate:   d,
		}
		if err := repository.Create(ctx, organisationID, p); err != nil {
			t.Fatalf("create payment: %v", err)
		}
		ids = append(ids, p.ID)
	}

	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = db.Exec(context.Background(), "DELETE FROM payments WHERE id = $1", id)
		}
	})

	payments, err := repository.GetByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}

	if len(payments) != 3 {
		t.Fatalf("expected 3 payments, got %d", len(payments))
	}

	wantOrder := []string{"2026-01-01", "2026-02-01", "2026-03-01"}
	for i, want := range wantOrder {
		got := payments[i].PaymentDate.Format("2006-01-02")
		if got != want {
			t.Errorf("position %d: expected date %s, got %s", i, want, got)
		}
	}
}

func TestPostgresPaymentRepository_GetTotalPaidByInvoiceID_NoPayments(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoice(t, db, organisationID, customerID)

	repository := NewPostgresPaymentRepository(db)

	total, err := repository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get total paid: %v", err)
	}

	if total != 0 {
		t.Errorf("expected total 0 for an invoice with no payments, got %d", total)
	}
}

func TestPostgresPaymentRepository_GetTotalPaidByInvoiceID_MultiplePayments(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoice(t, db, organisationID, customerID)

	repository := NewPostgresPaymentRepository(db)

	amounts := []int64{1000, 2500, 500}
	var ids []uuid.UUID
	for _, amount := range amounts {
		p := &Payment{
			ID:            uuid.New(),
			InvoiceID:     invoiceID,
			Amount:        amount,
			PaymentMethod: "cash",
			PaymentDate:   time.Now().UTC().Truncate(24 * time.Hour),
		}
		if err := repository.Create(ctx, organisationID, p); err != nil {
			t.Fatalf("create payment: %v", err)
		}
		ids = append(ids, p.ID)
	}

	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = db.Exec(context.Background(), "DELETE FROM payments WHERE id = $1", id)
		}
	})

	total, err := repository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get total paid: %v", err)
	}

	wantTotal := int64(1000 + 2500 + 500)
	if total != wantTotal {
		t.Errorf("expected total %d, got %d", wantTotal, total)
	}
}
