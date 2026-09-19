package invoice

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/product"
)

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
	// t.Cleanup (not defer) so this runs after row-delete cleanups
	// registered later: Cleanup callbacks fire last-added-first, all
	// after the test function's own defers have already run.
	t.Cleanup(func() { db.Close() })

	return db
}

// createTestOrganisation inserts a minimal organisation row directly (not
// via the administration package, to avoid a cross-package test
// dependency) and registers cleanup for it.
func createTestOrganisation(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()

	organisationID := uuid.New()

	_, err := db.Exec(
		context.Background(),
		"INSERT INTO organisations (id, name) VALUES ($1, $2)",
		organisationID,
		"Test Organisation",
	)
	if err != nil {
		t.Fatalf("create test organisation: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", organisationID)
	})

	return organisationID
}

// createTestCustomer uses the real customer package (already a production
// dependency of this package) to create a customer row and register
// cleanup for it.
func createTestCustomer(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) uuid.UUID {
	t.Helper()

	repository := customer.NewPostgresCustomerRepository(db)
	c := &customer.Customer{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Test Customer",
		Status:         "active",
	}

	if err := repository.Create(context.Background(), c); err != nil {
		t.Fatalf("create test customer: %v", err)
	}

	t.Cleanup(func() {
		// addresses (Milestone 7 Part 1) has no cleanup of its own here and
		// must be deleted before customers, or the customers delete below
		// silently fails on the addresses_customer_id_fkey foreign key —
		// same fix as cleanupOrganisation in internal/app/app_test.go.
		_, _ = db.Exec(context.Background(), "DELETE FROM addresses WHERE customer_id = $1", c.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM customers WHERE id = $1", c.ID)
	})

	return c.ID
}

// createTestProduct uses the real product package (already a production
// dependency of this package) to create a product row and register
// cleanup for it.
func createTestProduct(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) uuid.UUID {
	t.Helper()

	repository := product.NewPostgresProductRepository(db)
	p := &product.Product{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Test Product",
		SKU:            uuid.New().String(),
		Price:          500,
		IsActive:       true,
	}

	if err := repository.Create(context.Background(), p); err != nil {
		t.Fatalf("create test product: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM products WHERE id = $1", p.ID)
	})

	return p.ID
}

// createTestSettings uses the real administration package (a production
// dependency of the invoice service, for invoice number allocation) to
// create a settings row and register cleanup for it. Only tests that
// exercise InvoiceService.Create end-to-end need this — the repository's
// own tests below (Create/CreateLines called directly) never touch
// settings at all.
func createTestSettings(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) *admin.Settings {
	t.Helper()

	repository := admin.NewPostgresSettingsRepository(db)
	s := &admin.Settings{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		InvoicePrefix:  "INV-",
		InvoiceNumber:  0,
		Currency:       "GBP",
		PaymentTerms:   30,
	}

	if err := repository.Create(context.Background(), s); err != nil {
		t.Fatalf("create test settings: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM settings WHERE id = $1", s.ID)
	})

	return s
}

func TestPostgresInvoiceRepository_CreateAndGetByID(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	productID := createTestProduct(t, db, organisationID)

	repository := NewPostgresInvoiceRepository(db)

	issueDate, _ := parseRequiredDate("2026-01-01", nil, nil)
	dueDate, _ := parseRequiredDate("2026-01-31", nil, nil)
	notes := "Thanks for your business"

	inv := &Invoice{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		CustomerID:     customerID,
		InvoiceNumber:  "INV-TEST-1",
		IssueDate:      issueDate,
		DueDate:        dueDate,
		Subtotal:       2500,
		VATTotal:       500,
		Total:          3000,
		Status:         InvoiceStatusDraft,
		Notes:          &notes,
	}

	if err := repository.Create(ctx, inv); err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_lines WHERE invoice_id = $1", inv.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", inv.ID)
	})

	// One product-backed line with a fractional quantity/VAT rate (this
	// is the sharpest test of NUMERIC <-> float64 round-tripping through
	// pgx), and one custom line with no product.
	lines := []*Line{
		{
			ID:          uuid.New(),
			InvoiceID:   inv.ID,
			ProductID:   &productID,
			Description: "Product line",
			Quantity:    1.5,
			UnitPrice:   1000,
			VATRate:     17.5,
			VATAmount:   263, // round(1500 * 17.5 / 100) = round(262.5) = 263
			Total:       1763,
		},
		{
			ID:          uuid.New(),
			InvoiceID:   inv.ID,
			ProductID:   nil,
			Description: "Custom line",
			Quantity:    2.25,
			UnitPrice:   400,
			VATRate:     10,
			VATAmount:   90,
			Total:       990,
		},
	}

	if err := repository.CreateLines(ctx, lines); err != nil {
		t.Fatalf("create invoice lines: %v", err)
	}

	created, err := repository.GetByID(ctx, organisationID, inv.ID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if created.ID != inv.ID {
		t.Errorf("expected ID %v, got %v", inv.ID, created.ID)
	}

	if created.InvoiceNumber != inv.InvoiceNumber {
		t.Errorf("expected invoice number %q, got %q", inv.InvoiceNumber, created.InvoiceNumber)
	}

	if created.IssueDate.Format(dateLayout) != "2026-01-01" {
		t.Errorf("expected issue date 2026-01-01, got %s", created.IssueDate.Format(dateLayout))
	}

	if created.DueDate.Format(dateLayout) != "2026-01-31" {
		t.Errorf("expected due date 2026-01-31, got %s", created.DueDate.Format(dateLayout))
	}

	if created.Subtotal != 2500 || created.VATTotal != 500 || created.Total != 3000 {
		t.Errorf("expected subtotal=2500 vatTotal=500 total=3000, got subtotal=%d vatTotal=%d total=%d",
			created.Subtotal, created.VATTotal, created.Total)
	}

	if created.Status != InvoiceStatusDraft {
		t.Errorf("expected status %q, got %q", InvoiceStatusDraft, created.Status)
	}

	if created.Notes == nil || *created.Notes != notes {
		t.Errorf("expected notes %q, got %v", notes, created.Notes)
	}

	gotLines, err := repository.GetLinesByInvoiceID(ctx, organisationID, inv.ID)
	if err != nil {
		t.Fatalf("get invoice lines: %v", err)
	}

	if len(gotLines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(gotLines))
	}

	// Lines are ordered by created_at; both were inserted in the same
	// call, so compare by matching description rather than assuming order.
	byDescription := make(map[string]*Line, len(gotLines))
	for _, l := range gotLines {
		byDescription[l.Description] = l
	}

	productLine, ok := byDescription["Product line"]
	if !ok {
		t.Fatal("expected a line named \"Product line\"")
	}

	if productLine.ProductID == nil || *productLine.ProductID != productID {
		t.Errorf("expected product ID %v, got %v", productID, productLine.ProductID)
	}

	if productLine.Quantity != 1.5 {
		t.Errorf("expected quantity 1.5, got %v", productLine.Quantity)
	}

	if productLine.VATRate != 17.5 {
		t.Errorf("expected VAT rate 17.5, got %v", productLine.VATRate)
	}

	if productLine.VATAmount != 263 || productLine.Total != 1763 {
		t.Errorf("expected vatAmount=263 total=1763, got vatAmount=%d total=%d", productLine.VATAmount, productLine.Total)
	}

	customLine, ok := byDescription["Custom line"]
	if !ok {
		t.Fatal("expected a line named \"Custom line\"")
	}

	if customLine.ProductID != nil {
		t.Errorf("expected no product on the custom line, got %v", *customLine.ProductID)
	}

	if customLine.Quantity != 2.25 {
		t.Errorf("expected quantity 2.25, got %v", customLine.Quantity)
	}
}

func TestPostgresInvoiceRepository_GetByID_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresInvoiceRepository(db)

	inv, err := repository.GetByID(ctx, organisationID, uuid.New())
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound, got %v", err)
	}

	if inv != nil {
		t.Errorf("expected nil invoice, got %+v", inv)
	}
}

// TestPostgresInvoiceRepository_GetByID_OrganisationScoping proves that an
// invoice belonging to one organisation cannot be retrieved through a
// different organisation's ID.
func TestPostgresInvoiceRepository_GetByID_OrganisationScoping(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationA)

	repository := NewPostgresInvoiceRepository(db)

	issueDate, _ := parseRequiredDate("2026-01-01", nil, nil)
	dueDate, _ := parseRequiredDate("2026-01-31", nil, nil)

	inv := &Invoice{
		ID:             uuid.New(),
		OrganisationID: organisationA,
		CustomerID:     customerID,
		InvoiceNumber:  "INV-SCOPE-1",
		IssueDate:      issueDate,
		DueDate:        dueDate,
		Status:         InvoiceStatusDraft,
	}

	if err := repository.Create(ctx, inv); err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", inv.ID)
	})

	if _, err := repository.GetByID(ctx, organisationA, inv.ID); err != nil {
		t.Fatalf("get invoice via owning organisation: %v", err)
	}

	result, err := repository.GetByID(ctx, organisationB, inv.ID)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound when scoped to the wrong organisation, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil invoice, got %+v", result)
	}
}
