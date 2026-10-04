package invoice

import (
	"context"
	"errors"
	"testing"

	"go-invoicing/internal/customer"
)

// TestInvoiceService_Cancel_Draft_CapturesSnapshotAndSurvivesCustomerDelete
// proves the whole reason a cancelled Draft gets a snapshot: once
// cancelled, the invoice reads entirely from its own data, so its
// customer can then be deleted and the invoice still shows the customer's
// name, resolves its currency, and renders a PDF.
func TestInvoiceService_Cancel_Draft_CapturesSnapshotAndSurvivesCustomerDelete(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	if _, err := service.Cancel(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("cancel draft: %v", err)
	}

	customerRepository := customer.NewPostgresCustomerRepository(db)
	if err := customerRepository.SoftDelete(ctx, organisationID, customerID); err != nil {
		t.Fatalf("expected customer with only a cancelled invoice to be deletable, got %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.Status != InvoiceStatusCancelled {
		t.Errorf("expected status %q, got %q", InvoiceStatusCancelled, persisted.Status)
	}
	if persisted.CancelledAt == nil {
		t.Error("expected cancelled_at to be set")
	}
	if persisted.SentAt != nil {
		t.Error("expected sent_at to stay nil: a cancelled draft was never sent")
	}
	if persisted.CustomerName == nil || persisted.Currency == nil || len(persisted.RenderedTemplateSnapshot) == 0 {
		t.Fatal("expected cancelling a draft to capture the party, currency and template snapshot")
	}
	if persisted.CustomerDisplayName != "Test Customer" {
		t.Errorf("expected customer display name to survive customer deletion, got %q", persisted.CustomerDisplayName)
	}

	if _, _, _, currency, err := service.GetByID(ctx, organisationID, invoiceID); err != nil || currency == "" {
		t.Errorf("expected cancelled invoice to resolve its currency from its snapshot, got %q, %v", currency, err)
	}

	pdf, _, err := realPDFService(db).Generate(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("expected cancelled invoice PDF to render after customer deletion, got %v", err)
	}
	if len(pdf) == 0 {
		t.Error("expected non-empty PDF")
	}

	items, _, err := service.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusCancelled, Limit: 50}, persisted.CreatedAt)
	if err != nil {
		t.Fatalf("list cancelled invoices: %v", err)
	}
	if len(items) != 1 || items[0].Invoice.ID != invoiceID {
		t.Fatalf("expected the cancelled invoice in ?status=cancelled, got %d items", len(items))
	}
	if items[0].Invoice.CustomerDisplayName != "Test Customer" {
		t.Errorf("expected list row customer name %q, got %q", "Test Customer", items[0].Invoice.CustomerDisplayName)
	}
}

// TestInvoiceService_Cancel_SentUnpaid_KeepsSentSnapshot proves cancelling
// an issued invoice keeps the snapshot it was sent with, and that a
// cancelled invoice can't then be sent, paid, or cancelled again.
func TestInvoiceService_Cancel_SentUnpaid_KeepsSentSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	sent, err := service.Send(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if _, err := service.Cancel(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("cancel sent invoice: %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}
	if persisted.Status != InvoiceStatusCancelled {
		t.Errorf("expected status %q, got %q", InvoiceStatusCancelled, persisted.Status)
	}
	if persisted.SentAt == nil || *persisted.CustomerName != *sent.CustomerName || string(persisted.RenderedTemplateSnapshot) == "" {
		t.Error("expected the sent snapshot (sent_at, customer name, template) to be kept")
	}

	if _, err := service.Cancel(ctx, organisationID, invoiceID); !errors.Is(err, ErrInvoiceAlreadyCancelled) {
		t.Errorf("expected repeat cancel to fail with ErrInvoiceAlreadyCancelled, got %v", err)
	}
	if _, err := service.Send(ctx, organisationID, invoiceID); !errors.Is(err, ErrInvoiceAlreadyCancelled) {
		t.Errorf("expected send of cancelled invoice to fail with ErrInvoiceAlreadyCancelled, got %v", err)
	}
	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
		IdempotencyKey: newTestIdempotencyKey(),
		Amount:         1000,
		PaymentMethod:  "cash",
	}); !errors.Is(err, ErrInvoiceCannotAcceptPayment) {
		t.Errorf("expected payment on cancelled invoice to fail with ErrInvoiceCannotAcceptPayment, got %v", err)
	}
}

// TestInvoiceService_Cancel_PartPaid_Rejected proves money received
// blocks cancellation, and the rejection changes nothing.
func TestInvoiceService_Cancel_PartPaid_Rejected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusSent)

	service := newPaymentTestService(db)

	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
		IdempotencyKey: newTestIdempotencyKey(),
		Amount:         1000,
		PaymentMethod:  "cash",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if _, err := service.Cancel(ctx, organisationID, invoiceID); !errors.Is(err, ErrInvoiceCannotBeCancelled) {
		t.Fatalf("expected ErrInvoiceCannotBeCancelled, got %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}
	if persisted.Status != InvoiceStatusSent || persisted.CancelledAt != nil {
		t.Errorf("expected rejected cancel to leave the invoice Sent and uncancelled, got %q", persisted.Status)
	}
}

// TestPostgresCustomerRepository_SoftDelete_BlockedByOpenInvoice proves
// the customer delete rule against real SQL: an open (Draft) invoice
// blocks it; once that invoice is cancelled, deletion succeeds, and the
// customer is gone from lookups.
func TestPostgresCustomerRepository_SoftDelete_BlockedByOpenInvoice(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	customerRepository := customer.NewPostgresCustomerRepository(db)

	if err := customerRepository.SoftDelete(ctx, organisationID, customerID); !errors.Is(err, customer.ErrCustomerHasOpenInvoices) {
		t.Fatalf("expected ErrCustomerHasOpenInvoices, got %v", err)
	}
	if _, err := customerRepository.GetByID(ctx, organisationID, customerID); err != nil {
		t.Fatalf("expected blocked delete to leave the customer in place, got %v", err)
	}

	if _, err := newPaymentTestService(db).Cancel(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if err := customerRepository.SoftDelete(ctx, organisationID, customerID); err != nil {
		t.Fatalf("expected delete to succeed once the invoice is cancelled, got %v", err)
	}
	if _, err := customerRepository.GetByID(ctx, organisationID, customerID); !errors.Is(err, customer.ErrCustomerNotFound) {
		t.Errorf("expected deleted customer to be gone from GetByID, got %v", err)
	}
	if err := customerRepository.SoftDelete(ctx, organisationID, customerID); !errors.Is(err, customer.ErrCustomerNotFound) {
		t.Errorf("expected deleting an already-deleted customer to be ErrCustomerNotFound, got %v", err)
	}
}
