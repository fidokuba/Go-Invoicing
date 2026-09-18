package invoice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestInvoiceService_Send_Success(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if inv.Status != InvoiceStatusSent {
		t.Errorf("expected status %q, got %q", InvoiceStatusSent, inv.Status)
	}

	if inv.SentAt == nil {
		t.Fatal("expected SentAt to be populated")
	}

	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusSent {
		t.Errorf("expected persisted status %q, got %q", InvoiceStatusSent, persisted.Status)
	}

	if persisted.SentAt == nil {
		t.Error("expected persisted SentAt to be populated")
	}

	if !f.tx.committed {
		t.Error("expected the transaction to be committed")
	}

	if f.tx.rolledBack {
		t.Error("expected the transaction not to be rolled back")
	}
}

func TestInvoiceService_Send_NotFound(t *testing.T) {
	f := newTestFixture()

	_, err := f.service.Send(context.Background(), f.organisationID, uuid.New())
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound, got %v", err)
	}
}

// TestInvoiceService_Send_WrongOrganisation proves the existing
// organisation-scoped GetForUpdate behaviour (Milestone 4) is what Send
// relies on for tenant isolation — a Draft invoice under a different
// organisation is treated exactly like one that doesn't exist.
func TestInvoiceService_Send_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	_, err := f.service.Send(context.Background(), uuid.New(), invoiceID)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-organisation send, got %v", err)
	}

	// Must not have been transitioned via the correct organisation's view
	// either — the wrong-organisation attempt must not have mutated it.
	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusDraft, persisted.Status)
	}
}

func TestInvoiceService_Send_AlreadySent(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}
}

func TestInvoiceService_Send_Paid(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusPaid)

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusPaid {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusPaid, persisted.Status)
	}
}

// TestInvoiceService_Send_RejectedTransitionPerformsNoLifecycleUpdate
// proves a rejected Send never even calls the repository's MarkSent —
// the in-memory Invoice.MarkSent guard fails first, so there is no
// lifecycle write for the transaction to roll back in the first place.
func TestInvoiceService_Send_RejectedTransitionPerformsNoLifecycleUpdate(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	beforeSentAt := f.repository.invoices[invoiceID].SentAt

	if _, err := f.service.Send(context.Background(), f.organisationID, invoiceID); !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent, got %v", err)
	}

	afterSentAt := f.repository.invoices[invoiceID].SentAt
	if beforeSentAt != nil || afterSentAt != nil {
		t.Errorf("expected SentAt to remain nil throughout, got before=%v after=%v", beforeSentAt, afterSentAt)
	}
}

func TestInvoiceService_Send_BeginTransactionErrorPropagates(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.service.txBeginner = &fakeTxBeginner{beginErr: errors.New("pool exhausted")}

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}

	persisted := f.repository.invoices[invoiceID]
	if persisted.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusDraft, persisted.Status)
	}
}

func TestInvoiceService_Send_GetForUpdateErrorPropagates(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.repository.getForUpdateErr = errors.New("connection reset by peer")

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestInvoiceService_Send_RollsBackOnMarkSentFailure(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.repository.markSentErr = errors.New("connection reset by peer")

	_, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestInvoiceService_Send_CommitFailureDoesNotReportSuccess(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.tx.commitErr = errors.New("connection reset by peer")

	inv, err := f.service.Send(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error when commit fails")
	}

	if inv != nil {
		t.Errorf("expected a nil result on commit failure, got %+v", inv)
	}
}
