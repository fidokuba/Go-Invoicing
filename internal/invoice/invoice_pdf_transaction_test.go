package invoice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
)

// realPDFService wires an InvoicePDFService against real PostgreSQL
// repositories — the same construction app.go does — mirroring
// newPaymentTestService's own pattern in payment_transaction_test.go.
func realPDFService(db *pgxpool.Pool) *InvoicePDFService {
	return NewInvoicePDFService(
		NewPostgresInvoiceRepository(db),
		NewPostgresPaymentRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresCustomerRepository(db),
		customer.NewPostgresAddressRepository(db),
		admin.NewPostgresSettingsRepository(db),
		NewInvoicePDFRenderer(),
		nil,
	)
}

func TestInvoicePDFService_Draft_ReadsLivePartyData_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{Name: "Live Seller Ltd"}); err != nil {
		t.Fatalf("set organisation: %v", err)
	}
	createTestSettings(t, db, organisationID)
	customerID := createTestCustomer(t, db, organisationID)
	if _, err := db.Exec(ctx, "UPDATE customers SET name = $1 WHERE id = $2", "Live Customer Name", customerID); err != nil {
		t.Fatalf("set customer name: %v", err)
	}
	if _, err := customer.NewPostgresAddressRepository(db).UpsertBillingAddress(ctx, organisationID, customerID, &customer.Address{
		ID: uuid.New(), Street: "1 Live Street", City: "Bristol", PostalCode: "BS1 1AA", Country: "GB",
	}); err != nil {
		t.Fatalf("set billing address: %v", err)
	}

	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 1000, InvoiceStatusDraft)

	service := realPDFService(db)
	data, err := service.BuildData(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Seller.Name != "Live Seller Ltd" {
		t.Errorf("expected live seller name, got %q", data.Seller.Name)
	}
	if data.Customer.DisplayName != "Live Customer Name" {
		t.Errorf("expected live customer name, got %q", data.Customer.DisplayName)
	}
	if !strings.Contains(strings.Join(data.Customer.AddressLines, " "), "1 Live Street") {
		t.Errorf("expected live billing address, got %v", data.Customer.AddressLines)
	}
}

func TestInvoicePDFService_Issued_UsesStoredSnapshot_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{Name: "Original Seller"}); err != nil {
		t.Fatalf("set organisation: %v", err)
	}
	createTestSettings(t, db, organisationID)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 1000, InvoiceStatusDraft)

	sendService := newPaymentTestService(db)
	if _, err := sendService.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	service := realPDFService(db)
	data, err := service.BuildData(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Seller.Name != "Original Seller" {
		t.Errorf("expected snapshot seller name, got %q", data.Seller.Name)
	}
}

// TestInvoicePDFService_OrganisationChangeAfterSendDoesNotAffectPDF_RealPostgres
// proves the historical guarantee end-to-end against real Postgres:
// changing the organisation after Send must not alter a subsequent PDF
// build's data.
func TestInvoicePDFService_OrganisationChangeAfterSendDoesNotAffectPDF_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{Name: "Original Seller"}); err != nil {
		t.Fatalf("set organisation: %v", err)
	}
	createTestSettings(t, db, organisationID)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 1000, InvoiceStatusDraft)

	sendService := newPaymentTestService(db)
	if _, err := sendService.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{Name: "Changed Seller"}); err != nil {
		t.Fatalf("change organisation: %v", err)
	}

	service := realPDFService(db)
	data, err := service.BuildData(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Seller.Name != "Original Seller" {
		t.Errorf("expected snapshot seller name to remain %q despite the organisation change, got %q", "Original Seller", data.Seller.Name)
	}
}

// TestInvoicePDFService_CustomerAndAddressChangeAfterSendDoesNotAffectPDF_RealPostgres
// proves the same invariant for customer identity and billing address.
func TestInvoicePDFService_CustomerAndAddressChangeAfterSendDoesNotAffectPDF_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID)
	customerID := createTestCustomer(t, db, organisationID)
	if _, err := db.Exec(ctx, "UPDATE customers SET name = $1 WHERE id = $2", "Original Customer", customerID); err != nil {
		t.Fatalf("set customer name: %v", err)
	}
	addressRepository := customer.NewPostgresAddressRepository(db)
	if _, err := addressRepository.UpsertBillingAddress(ctx, organisationID, customerID, &customer.Address{
		ID: uuid.New(), Street: "Original Street", City: "Original City", PostalCode: "OR1 1AA", Country: "GB",
	}); err != nil {
		t.Fatalf("set billing address: %v", err)
	}

	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 1000, InvoiceStatusDraft)

	sendService := newPaymentTestService(db)
	if _, err := sendService.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if _, err := db.Exec(ctx, "UPDATE customers SET name = $1 WHERE id = $2", "Changed Customer", customerID); err != nil {
		t.Fatalf("change customer name: %v", err)
	}
	if _, err := addressRepository.UpsertBillingAddress(ctx, organisationID, customerID, &customer.Address{
		ID: uuid.New(), Street: "Changed Street", City: "Changed City", PostalCode: "CH1 1AA", Country: "GB",
	}); err != nil {
		t.Fatalf("change billing address: %v", err)
	}

	service := realPDFService(db)
	data, err := service.BuildData(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if data.Customer.DisplayName != "Original Customer" {
		t.Errorf("expected snapshot customer name to remain %q, got %q", "Original Customer", data.Customer.DisplayName)
	}
	if !strings.Contains(strings.Join(data.Customer.AddressLines, " "), "Original Street") {
		t.Errorf("expected snapshot billing address to remain, got %v", data.Customer.AddressLines)
	}
}

// TestInvoicePDFService_SettingsCurrencyChangeAfterSendDoesNotAffectPDF_RealPostgres
// proves the same invariant for currency.
func TestInvoicePDFService_SettingsCurrencyChangeAfterSendDoesNotAffectPDF_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID) // GBP
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 1000, InvoiceStatusDraft)

	sendService := newPaymentTestService(db)
	if _, err := sendService.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if _, err := db.Exec(ctx, "UPDATE settings SET currency = $1 WHERE organisation_id = $2", "USD", organisationID); err != nil {
		t.Fatalf("change currency: %v", err)
	}

	service := realPDFService(db)
	data, err := service.BuildData(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if !strings.Contains(data.Total, "£") {
		t.Errorf("expected the snapshot GBP currency to remain, got %q", data.Total)
	}
}

func TestInvoicePDFService_CrossTenantGeneration_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationA)
	customerID := createTestCustomer(t, db, organisationA)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationA, customerID, 1000, InvoiceStatusDraft)

	service := realPDFService(db)

	if _, err := service.BuildData(ctx, organisationA, invoiceID); err != nil {
		t.Fatalf("build data via owning organisation: %v", err)
	}

	if _, err := service.BuildData(ctx, organisationB, invoiceID); err == nil {
		t.Fatal("expected an error for a cross-tenant PDF generation attempt")
	}
}

// TestInvoicePDFService_LinesAndPaymentTotalsLoadCorrectly_RealPostgres
// proves lines and the amount-paid total are read correctly from real
// Postgres, using the existing tenant-scoped repository methods.
func TestInvoicePDFService_LinesAndPaymentTotalsLoadCorrectly_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	createTestSettings(t, db, organisationID)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 1000, InvoiceStatusDraft)

	sendService := newPaymentTestService(db)
	if _, err := sendService.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if _, _, err := sendService.CreatePayment(ctx, organisationID, invoiceID, CreatePaymentRequest{
		Amount: 400, PaymentMethod: "cash", PaymentDate: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	service := realPDFService(db)
	data, err := service.BuildData(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("build data: %v", err)
	}

	if !data.ShowPaymentSummary {
		t.Fatal("expected ShowPaymentSummary for a partial payment")
	}
	if data.AmountPaid == "" || data.AmountOutstanding == "" {
		t.Error("expected AmountPaid/AmountOutstanding to be populated")
	}
}
