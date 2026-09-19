package invoice

import (
	"context"
	"fmt"
	"sync"
	"testing"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/product"
)

// TestInvoiceService_Create_CommitsAllRowsTogether proves the success case
// against real PostgreSQL: the invoice row and every one of its lines are
// actually persisted, not just returned in memory, once Create returns
// without error — and the invoice number is the organisation's configured
// prefix plus the allocated sequential number.
func TestInvoiceService_Create_CommitsAllRowsTogether(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)

	invoiceRepository := NewPostgresInvoiceRepository(db)
	service := NewInvoiceService(
		invoiceRepository,
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresAddressRepository(db),
		admin.NewPostgresSettingsRepository(db),
		NewPostgresPaymentRepository(db),
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

	if inv.InvoiceNumber != "INV-1" {
		t.Errorf("expected invoice number %q, got %q", "INV-1", inv.InvoiceNumber)
	}

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

	if persisted.InvoiceNumber != "INV-1" {
		t.Errorf("expected persisted invoice number %q, got %q", "INV-1", persisted.InvoiceNumber)
	}

	if persisted.Total != inv.Total {
		t.Errorf("expected persisted total %d, got %d", inv.Total, persisted.Total)
	}

	persistedLines, err := invoiceRepository.GetLinesByInvoiceID(ctx, organisationID, inv.ID)
	if err != nil {
		t.Fatalf("get persisted lines: %v", err)
	}

	if len(persistedLines) != 2 {
		t.Errorf("expected 2 persisted lines, got %d", len(persistedLines))
	}
}

// TestInvoiceService_Create_SequentialInvoiceNumbers proves that two
// invoices created one after another for the same organisation get
// consecutive numbers.
func TestInvoiceService_Create_SequentialInvoiceNumbers(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)

	service := NewInvoiceService(
		NewPostgresInvoiceRepository(db),
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresAddressRepository(db),
		admin.NewPostgresSettingsRepository(db),
		NewPostgresPaymentRepository(db),
		db,
	)

	request := func() CreateInvoiceRequest {
		return CreateInvoiceRequest{
			CustomerID: customerID.String(),
			IssueDate:  "2026-01-01",
			DueDate:    "2026-01-31",
			Lines: []CreateInvoiceLineRequest{
				{Description: "Line", Quantity: 1, UnitPrice: 1000, VATRate: 20},
			},
		}
	}

	first, _, err := service.Create(ctx, organisationID, request())
	if err != nil {
		t.Fatalf("create first invoice: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", first.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", first.ID)
	})

	second, _, err := service.Create(ctx, organisationID, request())
	if err != nil {
		t.Fatalf("create second invoice: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", second.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", second.ID)
	})

	if first.InvoiceNumber != "INV-1" {
		t.Errorf("expected first invoice number %q, got %q", "INV-1", first.InvoiceNumber)
	}

	if second.InvoiceNumber != "INV-2" {
		t.Errorf("expected second invoice number %q, got %q", "INV-2", second.InvoiceNumber)
	}
}

// TestInvoiceService_Create_IndependentSequencesPerOrganisation proves
// invoice numbers are sequential per organisation, not globally shared:
// two different organisations both start at 1.
func TestInvoiceService_Create_IndependentSequencesPerOrganisation(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerA := createTestCustomer(t, db, organisationA)
	customerB := createTestCustomer(t, db, organisationB)
	createTestSettings(t, db, organisationA)
	createTestSettings(t, db, organisationB)

	service := NewInvoiceService(
		NewPostgresInvoiceRepository(db),
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresAddressRepository(db),
		admin.NewPostgresSettingsRepository(db),
		NewPostgresPaymentRepository(db),
		db,
	)

	newRequest := func(customerID string) CreateInvoiceRequest {
		return CreateInvoiceRequest{
			CustomerID: customerID,
			IssueDate:  "2026-01-01",
			DueDate:    "2026-01-31",
			Lines: []CreateInvoiceLineRequest{
				{Description: "Line", Quantity: 1, UnitPrice: 1000, VATRate: 20},
			},
		}
	}

	invA1, _, err := service.Create(ctx, organisationA, newRequest(customerA.String()))
	if err != nil {
		t.Fatalf("create organisation A's first invoice: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", invA1.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", invA1.ID)
	})

	invA2, _, err := service.Create(ctx, organisationA, newRequest(customerA.String()))
	if err != nil {
		t.Fatalf("create organisation A's second invoice: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", invA2.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", invA2.ID)
	})

	invB1, _, err := service.Create(ctx, organisationB, newRequest(customerB.String()))
	if err != nil {
		t.Fatalf("create organisation B's first invoice: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", invB1.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", invB1.ID)
	})

	if invA1.InvoiceNumber != "INV-1" {
		t.Errorf("expected organisation A's first invoice number %q, got %q", "INV-1", invA1.InvoiceNumber)
	}

	if invA2.InvoiceNumber != "INV-2" {
		t.Errorf("expected organisation A's second invoice number %q, got %q", "INV-2", invA2.InvoiceNumber)
	}

	if invB1.InvoiceNumber != "INV-1" {
		t.Errorf("expected organisation B's first invoice number %q (independent of A's sequence), got %q", "INV-1", invB1.InvoiceNumber)
	}
}

// TestInvoiceService_Create_RollsBackAtomicallyOnLineFailure proves
// atomicity across the whole transaction, including invoice number
// allocation: when the second of two lines fails to insert, neither the
// invoice row, nor the first (already-inserted-within-the-same-transaction)
// line, nor the settings update remain — the organisation's invoice_number
// is exactly as it was before this call, so the allocated number is not
// consumed by a failed creation.
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
	settings := createTestSettings(t, db, organisationID)

	settingsRepository := admin.NewPostgresSettingsRepository(db)
	// Start from a non-zero number, matching the milestone's own worked
	// example (settings number = 10, allocate 11, fail, still 10).
	if err := settingsRepository.UpdateInvoiceNumber(ctx, organisationID, 10); err != nil {
		t.Fatalf("seed settings invoice number: %v", err)
	}
	settings.InvoiceNumber = 10

	service := NewInvoiceService(
		NewPostgresInvoiceRepository(db),
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresAddressRepository(db),
		settingsRepository,
		NewPostgresPaymentRepository(db),
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

	// The crux of requirement 10.D: the settings row's invoice_number
	// must be back to 10, not 11 — proving the allocation itself, made
	// inside the same now-rolled-back transaction, was undone too.
	gotSettings, err := settingsRepository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	if gotSettings.InvoiceNumber != 10 {
		t.Errorf("expected settings invoice_number to remain 10 after rollback, got %d — the allocation was not rolled back", gotSettings.InvoiceNumber)
	}
}

// TestInvoiceService_Create_ConcurrentInvoiceCreation proves the actual
// concurrency-safety goal of this milestone part: many simultaneous
// invoice creations for the same organisation must all succeed, each
// with a unique number, forming the expected sequence — with no reliance
// on any particular goroutine scheduling order.
func TestInvoiceService_Create_ConcurrentInvoiceCreation(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)

	service := NewInvoiceService(
		NewPostgresInvoiceRepository(db),
		customer.NewPostgresCustomerRepository(db),
		product.NewPostgresProductRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresAddressRepository(db),
		admin.NewPostgresSettingsRepository(db),
		NewPostgresPaymentRepository(db),
		db,
	)

	const concurrency = 10

	var wg sync.WaitGroup
	invoices := make([]*Invoice, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			request := CreateInvoiceRequest{
				CustomerID: customerID.String(),
				IssueDate:  "2026-01-01",
				DueDate:    "2026-01-31",
				Lines: []CreateInvoiceLineRequest{
					{Description: "Concurrent line", Quantity: 1, UnitPrice: 1000, VATRate: 20},
				},
			}

			inv, _, err := service.Create(ctx, organisationID, request)
			invoices[i] = inv
			errs[i] = err
		}(i)
	}

	wg.Wait()

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id IN (SELECT id FROM invoices WHERE organisation_id = $1)", organisationID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE organisation_id = $1", organisationID)
	})

	seenNumbers := make(map[string]bool, concurrency)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: create invoice failed (no duplicate-number or other error is expected): %v", i, err)
		}

		if seenNumbers[invoices[i].InvoiceNumber] {
			t.Fatalf("duplicate invoice number allocated: %s", invoices[i].InvoiceNumber)
		}

		seenNumbers[invoices[i].InvoiceNumber] = true
	}

	if len(seenNumbers) != concurrency {
		t.Fatalf("expected %d unique invoice numbers, got %d", concurrency, len(seenNumbers))
	}

	// The numbers must form exactly the sequence 1..concurrency — not
	// necessarily allocated in goroutine-launch order (concurrent
	// creations can win the row lock in any order), but with no gaps and
	// no values outside the expected range.
	for i := 1; i <= concurrency; i++ {
		want := fmt.Sprintf("INV-%d", i)
		if !seenNumbers[want] {
			t.Errorf("expected invoice number %q to have been allocated, but it was not", want)
		}
	}

	// Cross-check against the settings row itself: it must land on
	// exactly concurrency, not more (a lost update would under-count) and
	// not less (a stuck/failed allocation would under-count too).
	settingsRepository := admin.NewPostgresSettingsRepository(db)
	finalSettings, err := settingsRepository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	if finalSettings.InvoiceNumber != concurrency {
		t.Errorf("expected final settings invoice_number %d, got %d", concurrency, finalSettings.InvoiceNumber)
	}
}
