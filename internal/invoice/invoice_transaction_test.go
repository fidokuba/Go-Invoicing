package invoice

import (
	"context"
	"testing"

	"go-invoicing/internal/customer"
	"go-invoicing/internal/product"
)

// TestInvoiceService_Create_CommitsAllRowsTogether proves the success case
// against real PostgreSQL: the invoice row and every one of its lines are
// actually persisted, not just returned in memory, once Create returns
// without error.
func TestInvoiceService_Create_CommitsAllRowsTogether(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)

	invoiceRepository := NewPostgresInvoiceRepository(db)
	service := NewInvoiceService(
		invoiceRepository,
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		db,
	)

	request := CreateInvoiceRequest{
		CustomerID: customerID.String(),
		IssueDate:  "2026-01-01",
		DueDate:    "2026-01-31",
		Lines: []CreateInvoiceLineRequest{
			{Description: "Line 1", Quantity: 1, UnitPrice: 1000, VATRate: 20},
			{Description: "Line 2", Quantity: 2, UnitPrice: 500, VATRate: 10},
		},
	}

	inv, lines, err := service.Create(ctx, organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", inv.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", inv.ID)
	})

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines returned, got %d", len(lines))
	}

	// Read back through a fresh call rather than trusting the in-memory
	// return values, to confirm the transaction actually committed.
	persisted, err := invoiceRepository.GetByID(ctx, organisationID, inv.ID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.ID != inv.ID {
		t.Errorf("expected persisted invoice ID %v, got %v", inv.ID, persisted.ID)
	}

	if persisted.Total != inv.Total {
		t.Errorf("expected persisted total %d, got %d", inv.Total, persisted.Total)
	}

	persistedLines, err := invoiceRepository.GetLinesByInvoiceID(ctx, inv.ID)
	if err != nil {
		t.Fatalf("get persisted lines: %v", err)
	}

	if len(persistedLines) != 2 {
		t.Errorf("expected 2 persisted lines, got %d", len(persistedLines))
	}
}

// TestInvoiceService_Create_RollsBackAtomicallyOnLineFailure proves
// atomicity: when the second of two lines fails to insert, neither the
// invoice row nor the first (already-inserted-within-the-same-transaction)
// line remain in PostgreSQL.
//
// The failure is a genuine, organically-reachable one, not a test-only
// hook: invoice_lines.vat_rate is NUMERIC(5,2), which can hold at most
// 999.99. InvoiceService's own validation only rejects a *negative* VAT
// rate — it has no upper bound — so a VAT rate far outside that range
// sails through service-level validation and fails only once CreateLines
// reaches the database. That gap is out of scope for this milestone (it's
// a validation completeness issue, not a transaction one) but is exactly
// the kind of late, DB-level failure the transaction must roll back
// cleanly regardless of its cause.
func TestInvoiceService_Create_RollsBackAtomicallyOnLineFailure(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)

	service := NewInvoiceService(
		NewPostgresInvoiceRepository(db),
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		db,
	)

	request := CreateInvoiceRequest{
		CustomerID: customerID.String(),
		IssueDate:  "2026-01-01",
		DueDate:    "2026-01-31",
		Lines: []CreateInvoiceLineRequest{
			// This line would succeed on its own — its INSERT commits
			// first, within the transaction, and must be undone by the
			// rollback the second line's failure triggers.
			{Description: "Line that would otherwise succeed", Quantity: 1, UnitPrice: 1000, VATRate: 20},
			// vat_rate NUMERIC(5,2) cannot hold a 6-digit value: this
			// INSERT fails with a numeric field overflow.
			{Description: "Line with an out-of-range VAT rate", Quantity: 1, UnitPrice: 1000, VATRate: 100000},
		},
	}

	_, _, err := service.Create(ctx, organisationID, request)
	if err == nil {
		t.Fatal("expected an error from the second line's database constraint violation, got nil")
	}

	var invoiceCount int
	if err := db.QueryRow(
		ctx,
		"SELECT count(*) FROM invoices WHERE organisation_id = $1",
		organisationID,
	).Scan(&invoiceCount); err != nil {
		t.Fatalf("count invoices: %v", err)
	}

	if invoiceCount != 0 {
		t.Errorf("expected 0 invoices after rollback, got %d — the invoice row was not rolled back", invoiceCount)
	}

	var lineCount int
	if err := db.QueryRow(
		ctx,
		`SELECT count(*) FROM invoice_lines il
		 JOIN invoices i ON i.id = il.invoice_id
		 WHERE i.organisation_id = $1`,
		organisationID,
	).Scan(&lineCount); err != nil {
		t.Fatalf("count invoice lines: %v", err)
	}

	if lineCount != 0 {
		t.Errorf("expected 0 invoice lines after rollback, got %d — the first line was not rolled back", lineCount)
	}
}
