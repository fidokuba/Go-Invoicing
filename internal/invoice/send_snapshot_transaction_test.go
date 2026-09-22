package invoice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
)

// TestInvoiceService_Send_PersistsCompleteSnapshot is the real-Postgres
// proof that every snapshot column — not just status/sent_at — is
// actually written, reading back through a fresh repository call rather
// than trusting the in-memory return value.
func TestInvoiceService_Send_PersistsCompleteSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	email, taxID := "seller@acme.test", "GB123456789"
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{
		Name: "Acme Ltd", Email: &email, TaxID: &taxID,
	}); err != nil {
		t.Fatalf("set organisation party data: %v", err)
	}

	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)

	if _, err := customer.NewPostgresAddressRepository(db).UpsertBillingAddress(ctx, organisationID, customerID, &customer.Address{
		ID: uuid.New(), Street: "2 Bakery Street", City: "Manchester", PostalCode: "M1 1AE", Country: "GB",
	}); err != nil {
		t.Fatalf("set billing address: %v", err)
	}

	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)
	if _, err := service.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.SellerName == nil || *persisted.SellerName != "Acme Ltd" {
		t.Errorf("expected SellerName %q, got %v", "Acme Ltd", persisted.SellerName)
	}
	if persisted.SellerEmail == nil || *persisted.SellerEmail != email {
		t.Errorf("expected SellerEmail %q, got %v", email, persisted.SellerEmail)
	}
	if persisted.SellerTaxID == nil || *persisted.SellerTaxID != taxID {
		t.Errorf("expected SellerTaxID %q, got %v", taxID, persisted.SellerTaxID)
	}
	if persisted.CustomerName == nil || *persisted.CustomerName == "" {
		t.Error("expected CustomerName to be captured")
	}
	if persisted.CustomerAddress == nil || *persisted.CustomerAddress != "2 Bakery Street" {
		t.Errorf("expected CustomerAddress %q, got %v", "2 Bakery Street", persisted.CustomerAddress)
	}
	if persisted.CustomerCity == nil || *persisted.CustomerCity != "Manchester" {
		t.Errorf("expected CustomerCity %q, got %v", "Manchester", persisted.CustomerCity)
	}
	if persisted.Currency == nil || *persisted.Currency != "GBP" {
		t.Errorf("expected Currency %q, got %v", "GBP", persisted.Currency)
	}
}

// TestInvoiceService_Send_FailureBeforeFinalUpdateLeavesDraftWithNoSnapshot
// proves that if Send fails before ever reaching MarkSentWithSnapshot —
// here, because the organisation has no resolvable name — the invoice
// remains a plain, snapshot-less Draft: no column is partially written.
func TestInvoiceService_Send_FailureBeforeFinalUpdateLeavesDraftWithNoSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	// Deliberately no createTestSettings call: Send must fail with
	// ErrInvoiceSettingsNotFound before ever building a snapshot.
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)
	_, err := service.Send(ctx, organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceSettingsNotFound) {
		t.Fatalf("expected ErrInvoiceSettingsNotFound, got %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.Status != InvoiceStatusDraft {
		t.Errorf("expected status to remain %q, got %q", InvoiceStatusDraft, persisted.Status)
	}
	if persisted.SentAt != nil {
		t.Error("expected SentAt to remain nil")
	}
	if persisted.SellerName != nil || persisted.CustomerName != nil || persisted.Currency != nil {
		t.Error("expected every snapshot column to remain NULL after a failed send")
	}
}

// TestInvoiceService_Send_OrganisationChangesAfterSendDoNotAlterSnapshot
// is Milestone 7 Part 2's central historical-correctness proof for the
// seller side: changing the organisation's Name/Address/Email/TaxID
// after an invoice has been Sent must never be reflected on that
// invoice's already-captured snapshot.
func TestInvoiceService_Send_OrganisationChangesAfterSendDoNotAlterSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	originalEmail := "original@acme.test"
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{
		Name: "Original Seller Name", Email: &originalEmail, Address: strPtr("1 Original Street"),
	}); err != nil {
		t.Fatalf("set original organisation party data: %v", err)
	}

	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)
	if _, err := service.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	// Organisation changes: Name, Address, Email, TaxID — all after Send.
	newEmail, newTaxID := "changed@acme.test", "GB999999973"
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{
		Name: "Changed Seller Name", Email: &newEmail, Address: strPtr("2 Changed Avenue"), TaxID: &newTaxID,
	}); err != nil {
		t.Fatalf("change organisation party data: %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if *persisted.SellerName != "Original Seller Name" {
		t.Errorf("expected snapshot SellerName to remain %q, got %q", "Original Seller Name", *persisted.SellerName)
	}
	if *persisted.SellerEmail != originalEmail {
		t.Errorf("expected snapshot SellerEmail to remain %q, got %q", originalEmail, *persisted.SellerEmail)
	}
	if *persisted.SellerAddress != "1 Original Street" {
		t.Errorf("expected snapshot SellerAddress to remain %q, got %q", "1 Original Street", *persisted.SellerAddress)
	}
	if persisted.SellerTaxID != nil {
		t.Errorf("expected snapshot SellerTaxID to remain nil (not present at Send time), got %v", *persisted.SellerTaxID)
	}
}

// TestInvoiceService_Send_CustomerChangesAfterSendDoNotAlterSnapshot
// changes the customer's identity fields directly via SQL (Milestone 7
// Part 1 deliberately has no customer-editing API), proving the same
// invariant on the customer side.
func TestInvoiceService_Send_CustomerChangesAfterSendDoNotAlterSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	if _, err := db.Exec(ctx, "UPDATE customers SET name = $1, email = $2 WHERE id = $3", "Original Customer Name", "original@bakery.test", customerID); err != nil {
		t.Fatalf("set original customer identity: %v", err)
	}
	createTestSettings(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)
	if _, err := service.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if _, err := db.Exec(ctx, "UPDATE customers SET name = $1, email = $2 WHERE id = $3", "Changed Customer Name", "changed@bakery.test", customerID); err != nil {
		t.Fatalf("change customer identity: %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if *persisted.CustomerName != "Original Customer Name" {
		t.Errorf("expected snapshot CustomerName to remain %q, got %q", "Original Customer Name", *persisted.CustomerName)
	}
	if *persisted.CustomerEmail != "original@bakery.test" {
		t.Errorf("expected snapshot CustomerEmail to remain %q, got %q", "original@bakery.test", *persisted.CustomerEmail)
	}
}

// TestInvoiceService_Send_BillingAddressChangesAfterSendDoNotAlterSnapshot
// proves the same invariant for the customer's billing address,
// mutating it through the real PUT-backed upsert repository method after
// Send.
func TestInvoiceService_Send_BillingAddressChangesAfterSendDoNotAlterSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)

	addressRepository := customer.NewPostgresAddressRepository(db)
	if _, err := addressRepository.UpsertBillingAddress(ctx, organisationID, customerID, &customer.Address{
		ID: uuid.New(), Street: "Original Street", City: "Original City", PostalCode: "OR1 1AA", Country: "GB",
	}); err != nil {
		t.Fatalf("set original billing address: %v", err)
	}

	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)
	if _, err := service.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if _, err := addressRepository.UpsertBillingAddress(ctx, organisationID, customerID, &customer.Address{
		ID: uuid.New(), Street: "Changed Street", City: "Changed City", PostalCode: "CH1 1AA", Country: "GB",
	}); err != nil {
		t.Fatalf("change billing address: %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if *persisted.CustomerAddress != "Original Street" {
		t.Errorf("expected snapshot CustomerAddress to remain %q, got %q", "Original Street", *persisted.CustomerAddress)
	}
	if *persisted.CustomerCity != "Original City" {
		t.Errorf("expected snapshot CustomerCity to remain %q, got %q", "Original City", *persisted.CustomerCity)
	}
}

// TestInvoiceService_Send_SettingsCurrencyChangeAfterSendDoesNotAlterSnapshot
// proves the same invariant for currency — Settings has no HTTP-facing
// update path (Milestone 7 Part 1/2 both explicitly exclude a settings
// API), so this mutates the column directly via SQL, exactly the way a
// future settings feature eventually would.
func TestInvoiceService_Send_SettingsCurrencyChangeAfterSendDoesNotAlterSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID) // GBP

	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)
	if _, err := service.Send(ctx, organisationID, invoiceID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	if _, err := db.Exec(ctx, "UPDATE settings SET currency = $1 WHERE organisation_id = $2", "EUR", organisationID); err != nil {
		t.Fatalf("change settings currency: %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if *persisted.Currency != "GBP" {
		t.Errorf("expected snapshot Currency to remain %q, got %q", "GBP", *persisted.Currency)
	}
}

// TestInvoiceService_Send_RepeatedSendPreservesOriginalSnapshot is the
// real-Postgres counterpart to the fake-based
// TestInvoiceService_Send_RepeatedSendNeverReloadsSnapshotSources: change
// every source record between the first and (rejected) second Send, and
// confirm none of it leaks into the already-captured snapshot.
func TestInvoiceService_Send_RepeatedSendPreservesOriginalSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{Name: "Original Seller"}); err != nil {
		t.Fatalf("set organisation name: %v", err)
	}
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)
	first, err := service.Send(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}

	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{Name: "Changed Seller"}); err != nil {
		t.Fatalf("change organisation name: %v", err)
	}
	if _, err := db.Exec(ctx, "UPDATE settings SET currency = $1 WHERE organisation_id = $2", "USD", organisationID); err != nil {
		t.Fatalf("change settings currency: %v", err)
	}

	_, err = service.Send(ctx, organisationID, invoiceID)
	if !errors.Is(err, ErrInvoiceAlreadySent) {
		t.Fatalf("expected ErrInvoiceAlreadySent on the repeat send, got %v", err)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if *persisted.SellerName != "Original Seller" {
		t.Errorf("expected snapshot SellerName to remain %q, got %q", "Original Seller", *persisted.SellerName)
	}
	if *persisted.Currency != "GBP" {
		t.Errorf("expected snapshot Currency to remain %q, got %q", "GBP", *persisted.Currency)
	}
	// PostgreSQL's timestamptz column only stores microsecond precision,
	// while Go's time.Now() (see InvoiceService.Send's sentAt :=
	// time.Now().UTC()) carries nanosecond precision — see
	// send_transaction_test.go's own comment on the identical comparison
	// for the full explanation. Truncating first.SentAt before comparing
	// is correct, not a weakened assertion.
	if !persisted.SentAt.Equal(first.SentAt.Truncate(time.Microsecond)) {
		t.Errorf("expected SentAt to remain %v, got %v", first.SentAt.Truncate(time.Microsecond), *persisted.SentAt)
	}
}

// TestInvoiceService_Send_ConcurrentSendYieldsOneImmutableSnapshot
// extends the existing concurrent-double-Send proof
// (TestInvoiceService_Send_ConcurrentSend_ExactlyOneSucceeds) with the
// Part 2-specific assertion: the single successful transaction's
// snapshot is exactly what ends up persisted, never a mix or a second
// capture.
func TestInvoiceService_Send_ConcurrentSendYieldsOneImmutableSnapshot(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	if err := admin.NewPostgresOrganisationRepository(db).Update(ctx, organisationID, &admin.Organisation{Name: "Concurrent Seller"}); err != nil {
		t.Fatalf("set organisation name: %v", err)
	}
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	service := newPaymentTestService(db)

	const attempts = 2
	var wg sync.WaitGroup
	errs := make([]error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := service.Send(ctx, organisationID, invoiceID)
			errs[i] = err
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, err := range errs {
		if err == nil {
			successCount++
		} else if !errors.Is(err, ErrInvoiceAlreadySent) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successCount != 1 {
		t.Fatalf("expected exactly 1 successful send, got %d", successCount)
	}

	persisted, err := NewPostgresInvoiceRepository(db).GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get persisted invoice: %v", err)
	}

	if persisted.SellerName == nil || *persisted.SellerName != "Concurrent Seller" {
		t.Errorf("expected snapshot SellerName %q, got %v", "Concurrent Seller", persisted.SellerName)
	}
	if persisted.Currency == nil || *persisted.Currency != "GBP" {
		t.Errorf("expected snapshot Currency %q, got %v", "GBP", persisted.Currency)
	}
}
