package invoice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// This file proves the Milestone 4 Part 6 repository-level hardening
// directly: every test here calls InvoiceRepository/PaymentRepository
// methods with a foreign organisationID, deliberately bypassing every
// service-level ownership check (InvoiceService.GetByID/GetForUpdate).
// The point is to prove the repository itself — not the service sitting
// in front of it — refuses to read or mutate another organisation's
// data. A future caller that forgot the service-level check would still
// be stopped here.

// TestPostgresInvoiceRepository_UpdateStatus_CrossTenantRejected proves
// UpdateStatus cannot change another organisation's invoice status, and
// that the invoice's status in the database is unchanged afterward.
func TestPostgresInvoiceRepository_UpdateStatus_CrossTenantRejected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)
	invoiceID := createTestInvoice(t, db, organisationA, customerID)

	repository := NewPostgresInvoiceRepository(db)

	err := repository.UpdateStatus(ctx, organisationB, invoiceID, InvoiceStatusPaid)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-organisation UpdateStatus, got %v", err)
	}

	persisted, err := repository.GetByID(ctx, organisationA, invoiceID)
	if err != nil {
		t.Fatalf("get invoice under its real organisation: %v", err)
	}

	if persisted.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q after the rejected cross-tenant update, got %q", InvoiceStatusDraft, persisted.Status)
	}
}

// TestPostgresInvoiceRepository_GetLinesByInvoiceID_CrossTenantRejected
// proves a foreign organisationID sees no lines at all — not an error,
// not a partial result — for an invoice it doesn't own, exactly as it
// would for an invoice with genuinely no lines.
func TestPostgresInvoiceRepository_GetLinesByInvoiceID_CrossTenantRejected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)
	invoiceID := createTestInvoice(t, db, organisationA, customerID)

	repository := NewPostgresInvoiceRepository(db)

	lines := []*Line{
		{
			ID:          uuid.New(),
			InvoiceID:   invoiceID,
			Description: "Consulting",
			Quantity:    1,
			UnitPrice:   1000,
			VATRate:     20,
			VATAmount:   200,
			Total:       1200,
		},
	}
	if err := repository.CreateLines(ctx, lines); err != nil {
		t.Fatalf("create invoice lines: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", invoiceID)
	})

	gotAsB, err := repository.GetLinesByInvoiceID(ctx, organisationB, invoiceID)
	if err != nil {
		t.Fatalf("get lines as the wrong organisation: %v", err)
	}
	if len(gotAsB) != 0 {
		t.Errorf("expected 0 lines for a cross-organisation lookup, got %d", len(gotAsB))
	}

	gotAsA, err := repository.GetLinesByInvoiceID(ctx, organisationA, invoiceID)
	if err != nil {
		t.Fatalf("get lines as the real organisation: %v", err)
	}
	if len(gotAsA) != 1 {
		t.Errorf("expected 1 line for the real organisation, got %d", len(gotAsA))
	}
}

// TestPostgresPaymentRepository_Create_CrossTenantRejected proves a
// payment cannot be inserted against an invoice belonging to a different
// organisation, and that no payment row is left behind afterward.
func TestPostgresPaymentRepository_Create_CrossTenantRejected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)
	invoiceID := createTestInvoice(t, db, organisationA, customerID)

	repository := NewPostgresPaymentRepository(db)

	payment := &Payment{
		ID:            uuid.New(),
		InvoiceID:     invoiceID,
		Amount:        500,
		PaymentMethod: "cash",
		PaymentDate:   time.Now().UTC().Truncate(24 * time.Hour),
	}

	err := repository.Create(ctx, organisationB, payment)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-organisation payment create, got %v", err)
	}

	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM payments WHERE invoice_id = $1", invoiceID).Scan(&count); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 payment rows after the rejected cross-tenant create, got %d", count)
	}
}

// TestPostgresPaymentRepository_GetByInvoiceID_CrossTenantRejected and
// TestPostgresPaymentRepository_GetTotalPaidByInvoiceID_CrossTenantRejected
// prove the two read paths behave identically to "no payments" for a
// foreign organisationID, never revealing the real payment data.
func TestPostgresPaymentRepository_GetByInvoiceID_CrossTenantRejected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)
	invoiceID := createTestInvoice(t, db, organisationA, customerID)

	repository := NewPostgresPaymentRepository(db)

	payment := &Payment{
		ID:            uuid.New(),
		InvoiceID:     invoiceID,
		Amount:        500,
		PaymentMethod: "cash",
		PaymentDate:   time.Now().UTC().Truncate(24 * time.Hour),
	}
	if err := repository.Create(ctx, organisationA, payment); err != nil {
		t.Fatalf("create payment as the real organisation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM payments WHERE id = $1", payment.ID)
	})

	gotAsB, err := repository.GetByInvoiceID(ctx, organisationB, invoiceID)
	if err != nil {
		t.Fatalf("get payments as the wrong organisation: %v", err)
	}
	if len(gotAsB) != 0 {
		t.Errorf("expected 0 payments for a cross-organisation lookup, got %d", len(gotAsB))
	}

	gotAsA, err := repository.GetByInvoiceID(ctx, organisationA, invoiceID)
	if err != nil {
		t.Fatalf("get payments as the real organisation: %v", err)
	}
	if len(gotAsA) != 1 {
		t.Errorf("expected 1 payment for the real organisation, got %d", len(gotAsA))
	}
}

func TestPostgresPaymentRepository_GetTotalPaidByInvoiceID_CrossTenantRejected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)
	invoiceID := createTestInvoice(t, db, organisationA, customerID)

	repository := NewPostgresPaymentRepository(db)

	payment := &Payment{
		ID:            uuid.New(),
		InvoiceID:     invoiceID,
		Amount:        750,
		PaymentMethod: "cash",
		PaymentDate:   time.Now().UTC().Truncate(24 * time.Hour),
	}
	if err := repository.Create(ctx, organisationA, payment); err != nil {
		t.Fatalf("create payment as the real organisation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM payments WHERE id = $1", payment.ID)
	})

	totalAsB, err := repository.GetTotalPaidByInvoiceID(ctx, organisationB, invoiceID)
	if err != nil {
		t.Fatalf("get total paid as the wrong organisation: %v", err)
	}
	if totalAsB != 0 {
		t.Errorf("expected total 0 for a cross-organisation lookup, got %d", totalAsB)
	}

	totalAsA, err := repository.GetTotalPaidByInvoiceID(ctx, organisationA, invoiceID)
	if err != nil {
		t.Fatalf("get total paid as the real organisation: %v", err)
	}
	if totalAsA != 750 {
		t.Errorf("expected total 750 for the real organisation, got %d", totalAsA)
	}
}
