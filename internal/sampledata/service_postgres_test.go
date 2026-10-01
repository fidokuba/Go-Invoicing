package sampledata_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"
	"go-invoicing/internal/sampledata"
	"go-invoicing/internal/template"
)

// This is an external test package (sampledata_test, not sampledata) for the
// same reason internal/renderer's own client_test.go is: wiring a real
// *invoice.InvoiceService here needs internal/invoice, and nothing about
// Service's own exported API requires reaching into its unexported
// internals to test it properly.

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

// createTestOrganisation mirrors internal/invoice's own test helper of
// the same name (duplicated here deliberately, same reasoning: avoiding
// a cross-package test-only dependency) — a minimal organisation row
// plus its permanent Classic template, since
// InvoicePDFService/InvoiceService both depend on every real
// organisation having one.
func createTestOrganisation(t *testing.T, db *pgxpool.Pool, vatRegistered bool) uuid.UUID {
	t.Helper()

	organisationID := uuid.New()

	_, err := db.Exec(
		context.Background(),
		"INSERT INTO organisations (id, name, vat_registered) VALUES ($1, $2, $3)",
		organisationID, "Test Data Seed Organisation", vatRegistered,
	)
	if err != nil {
		t.Fatalf("create test organisation: %v", err)
	}

	tx, err := db.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin create classic template: %v", err)
	}
	if err := template.CreateSystemTemplate(context.Background(), tx, organisationID); err != nil {
		t.Fatalf("create classic template: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit create classic template: %v", err)
	}

	settingsRepository := admin.NewPostgresSettingsRepository(db)
	if err := settingsRepository.Create(context.Background(), &admin.Settings{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		InvoicePrefix:  "INV-",
		InvoiceNumber:  0,
		Currency:       "GBP",
		PaymentTerms:   30,
	}); err != nil {
		t.Fatalf("create test settings: %v", err)
	}

	// One cascading cleanup, in FK-safe order, covering everything this
	// organisation's own Generate call(s) could possibly have created —
	// simpler and more robust than tracking every individual customer/
	// product/invoice ID this test's random generation produces.
	t.Cleanup(func() {
		ctx := context.Background()
		db.Exec(ctx, `DELETE FROM payments WHERE invoice_id IN (SELECT id FROM invoices WHERE organisation_id = $1)`, organisationID)
		db.Exec(ctx, `DELETE FROM invoice_lines WHERE invoice_id IN (SELECT id FROM invoices WHERE organisation_id = $1)`, organisationID)
		db.Exec(ctx, `DELETE FROM invoices WHERE organisation_id = $1`, organisationID)
		db.Exec(ctx, `DELETE FROM addresses WHERE customer_id IN (SELECT id FROM customers WHERE organisation_id = $1)`, organisationID)
		db.Exec(ctx, `DELETE FROM customers WHERE organisation_id = $1`, organisationID)
		db.Exec(ctx, `DELETE FROM products WHERE organisation_id = $1`, organisationID)
		db.Exec(ctx, `DELETE FROM invoice_templates WHERE organisation_id = $1`, organisationID)
		db.Exec(ctx, `DELETE FROM settings WHERE organisation_id = $1`, organisationID)
		db.Exec(ctx, `DELETE FROM organisations WHERE id = $1`, organisationID)
	})

	return organisationID
}

func newTestService(db *pgxpool.Pool) *sampledata.Service {
	organisationRepository := admin.NewPostgresOrganisationRepository(db)
	settingsRepository := admin.NewPostgresSettingsRepository(db)
	addressRepository := customer.NewPostgresAddressRepository(db)
	customerRepository := customer.NewPostgresCustomerRepository(db)
	productRepository := product.NewPostgresProductRepository(db)
	paymentRepository := invoice.NewPostgresPaymentRepository(db)
	templateRepository := template.NewPostgresTemplateRepository(db)
	invoiceRepository := invoice.NewPostgresInvoiceRepository(db)

	customerService := customer.NewCustomerService(customerRepository, addressRepository)
	productService := product.NewProductService(productRepository)
	invoiceService := invoice.NewInvoiceService(
		invoiceRepository, customerRepository, productRepository, organisationRepository,
		addressRepository, settingsRepository, paymentRepository, templateRepository, db,
	)

	return sampledata.NewService(organisationRepository, customerService, productService, invoiceService)
}

// countInvoiceLineGroups returns how many distinct line-counts exist
// among invoiceIDs' own lines — used below only to confirm both "one
// line" and "more than one line" genuinely occurred, without caring
// about the exact distribution.
func lineCounts(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) []int {
	t.Helper()

	rows, err := db.Query(context.Background(), `
		SELECT COUNT(*) FROM invoice_lines il
		JOIN invoices i ON i.id = il.invoice_id
		WHERE i.organisation_id = $1
		GROUP BY il.invoice_id
	`, organisationID)
	if err != nil {
		t.Fatalf("query line counts: %v", err)
	}
	defer rows.Close()

	var counts []int
	for rows.Next() {
		var c int
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan line count: %v", err)
		}
		counts = append(counts, c)
	}
	return counts
}

func TestService_Generate_CreatesExpectedShapeOfData(t *testing.T) {
	db := newTestPool(t)
	organisationID := createTestOrganisation(t, db, true)
	service := newTestService(db)

	summary, err := service.Generate(context.Background(), organisationID)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if summary.CustomersCreated != 5 {
		t.Errorf("expected exactly 5 customers, got %d", summary.CustomersCreated)
	}

	if summary.ProductsCreated < 6 || summary.ProductsCreated > 12 {
		t.Errorf("expected 6-12 products, got %d", summary.ProductsCreated)
	}

	// 5 customers * 1-4 invoices each.
	if summary.InvoicesCreated < 5 || summary.InvoicesCreated > 20 {
		t.Errorf("expected 5-20 invoices, got %d", summary.InvoicesCreated)
	}

	if summary.InvoicesSent < summary.InvoicesPaid {
		t.Errorf("expected at least as many sent invoices as paid ones, got sent=%d paid=%d", summary.InvoicesSent, summary.InvoicesPaid)
	}

	var customerCount int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM customers WHERE organisation_id = $1", organisationID).Scan(&customerCount); err != nil {
		t.Fatalf("count customers: %v", err)
	}
	if customerCount != 5 {
		t.Errorf("expected 5 customer rows in the database, got %d", customerCount)
	}

	var invoiceCount int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM invoices WHERE organisation_id = $1", organisationID).Scan(&invoiceCount); err != nil {
		t.Fatalf("count invoices: %v", err)
	}
	if invoiceCount != summary.InvoicesCreated {
		t.Errorf("expected %d invoice rows, got %d", summary.InvoicesCreated, invoiceCount)
	}

	// The feature's own explicit requirement: some invoices with exactly
	// one line, some with several — not left to chance (see
	// forcedLineCounts in service.go), so this must always hold, not just
	// usually.
	counts := lineCounts(t, db, organisationID)
	hasSingle, hasMultiple := false, false
	for _, c := range counts {
		if c == 1 {
			hasSingle = true
		}
		if c > 1 {
			hasMultiple = true
		}
	}
	if !hasSingle {
		t.Error("expected at least one invoice with exactly one line")
	}
	if !hasMultiple {
		t.Error("expected at least one invoice with more than one line")
	}

	// VAT-registered organisation: every generated line should carry the
	// 20% rate Generate uses for that case (never silently 0, which
	// would make this indistinguishable from the non-registered case).
	var nonTwentyPercentLines int
	if err := db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM invoice_lines il
		JOIN invoices i ON i.id = il.invoice_id
		WHERE i.organisation_id = $1 AND il.vat_rate != 20
	`, organisationID).Scan(&nonTwentyPercentLines); err != nil {
		t.Fatalf("count non-20%% VAT lines: %v", err)
	}
	if nonTwentyPercentLines != 0 {
		t.Errorf("expected every line to carry a 20%% VAT rate for a VAT-registered organisation, found %d that don't", nonTwentyPercentLines)
	}
}

func TestService_Generate_NonVATRegisteredOrganisationGetsZeroRateLines(t *testing.T) {
	db := newTestPool(t)
	organisationID := createTestOrganisation(t, db, false)
	service := newTestService(db)

	if _, err := service.Generate(context.Background(), organisationID); err != nil {
		t.Fatalf("generate: %v", err)
	}

	var nonZeroRateLines int
	if err := db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM invoice_lines il
		JOIN invoices i ON i.id = il.invoice_id
		WHERE i.organisation_id = $1 AND il.vat_rate != 0
	`, organisationID).Scan(&nonZeroRateLines); err != nil {
		t.Fatalf("count non-zero VAT lines: %v", err)
	}
	if nonZeroRateLines != 0 {
		t.Errorf("expected every line to carry a 0%% VAT rate for a non-VAT-registered organisation, found %d that don't", nonZeroRateLines)
	}
}

// TestService_Generate_CanBeCalledRepeatedly proves the per-run random
// suffix (runSuffix in service.go) actually does its job: a second call
// against the same organisation must not fail on products_organisation_
// sku_unique (migration 000006) or any other collision, and must add a
// genuinely new batch rather than silently no-op-ing.
func TestService_Generate_CanBeCalledRepeatedly(t *testing.T) {
	db := newTestPool(t)
	organisationID := createTestOrganisation(t, db, true)
	service := newTestService(db)

	first, err := service.Generate(context.Background(), organisationID)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}

	second, err := service.Generate(context.Background(), organisationID)
	if err != nil {
		t.Fatalf("second generate (should not collide with the first): %v", err)
	}

	var customerCount, productCount int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM customers WHERE organisation_id = $1", organisationID).Scan(&customerCount); err != nil {
		t.Fatalf("count customers: %v", err)
	}
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM products WHERE organisation_id = $1", organisationID).Scan(&productCount); err != nil {
		t.Fatalf("count products: %v", err)
	}

	if customerCount != 10 {
		t.Errorf("expected 10 customers after two runs (5 each), got %d", customerCount)
	}
	if productCount != first.ProductsCreated+second.ProductsCreated {
		t.Errorf("expected %d products after two runs, got %d", first.ProductsCreated+second.ProductsCreated, productCount)
	}
}
