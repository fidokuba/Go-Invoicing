package invoice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/product"
)

// fakeInvoiceRepository is an in-memory InvoiceRepository used to test the
// service/handler without touching PostgreSQL. WithTx ignores its tx
// argument and returns the same fake — the fake has no real transactional
// semantics of its own; that guarantee is proven separately against real
// PostgreSQL in invoice_transaction_test.go. createErr/createLinesErr let
// tests simulate a failure partway through persistence, to exercise the
// service's rollback path.
type fakeInvoiceRepository struct {
	invoices map[uuid.UUID]Invoice
	lines    map[uuid.UUID][]*Line

	createErr               error
	createLinesErr          error
	getByIDErr              error
	getForUpdateErr         error
	updateStatusErr         error
	markSentWithSnapshotErr error
	getLinesErr             error
}

func newFakeInvoiceRepository() *fakeInvoiceRepository {
	return &fakeInvoiceRepository{
		invoices: make(map[uuid.UUID]Invoice),
		lines:    make(map[uuid.UUID][]*Line),
	}
}

func (f *fakeInvoiceRepository) WithTx(tx pgx.Tx) InvoiceRepository {
	return f
}

func (f *fakeInvoiceRepository) Create(ctx context.Context, inv *Invoice) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.invoices[inv.ID] = *inv
	return nil
}

func (f *fakeInvoiceRepository) CreateLines(ctx context.Context, lines []*Line) error {
	if f.createLinesErr != nil {
		return f.createLinesErr
	}
	for _, l := range lines {
		f.lines[l.InvoiceID] = append(f.lines[l.InvoiceID], l)
	}
	return nil
}

func (f *fakeInvoiceRepository) GetByID(ctx context.Context, organisationID, invoiceID uuid.UUID) (*Invoice, error) {
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	inv, ok := f.invoices[invoiceID]
	if !ok || inv.OrganisationID != organisationID {
		return nil, ErrInvoiceNotFound
	}
	return &inv, nil
}

// GetLinesByInvoiceID mirrors the real repository's Milestone 4 Part 6
// organisation check: an invoice belonging to a different organisation
// yields an empty slice, exactly as GetByID's own not-found translation
// keeps cross-tenant lookups indistinguishable from "no lines."
func (f *fakeInvoiceRepository) GetLinesByInvoiceID(ctx context.Context, organisationID, invoiceID uuid.UUID) ([]*Line, error) {
	if f.getLinesErr != nil {
		return nil, f.getLinesErr
	}
	inv, ok := f.invoices[invoiceID]
	if !ok || inv.OrganisationID != organisationID {
		return nil, nil
	}
	return f.lines[invoiceID], nil
}

func (f *fakeInvoiceRepository) GetForUpdate(ctx context.Context, organisationID, invoiceID uuid.UUID) (*Invoice, error) {
	if f.getForUpdateErr != nil {
		return nil, f.getForUpdateErr
	}
	inv, ok := f.invoices[invoiceID]
	if !ok || inv.OrganisationID != organisationID {
		return nil, ErrInvoiceNotFound
	}
	return &inv, nil
}

// UpdateStatus mirrors the real repository's Milestone 4 Part 6
// organisation check: an invoice that exists but belongs to a different
// organisation is treated identically to one that doesn't exist at all.
func (f *fakeInvoiceRepository) UpdateStatus(ctx context.Context, organisationID, invoiceID uuid.UUID, status string) error {
	if f.updateStatusErr != nil {
		return f.updateStatusErr
	}
	inv, ok := f.invoices[invoiceID]
	if !ok || inv.OrganisationID != organisationID {
		return ErrInvoiceNotFound
	}
	inv.Status = status
	f.invoices[invoiceID] = inv
	return nil
}

// MarkSentWithSnapshot mirrors the real repository's Milestone 4 Part 6
// organisation check and Milestone 5/7-Part-2 atomic
// status+sent_at+snapshot write: an invoice that exists but belongs to a
// different organisation is treated identically to one that doesn't
// exist at all, and every field is copied from inv (the already-mutated
// value InvoiceService.Send's own Invoice.MarkSent call produced)
// together.
func (f *fakeInvoiceRepository) MarkSentWithSnapshot(ctx context.Context, organisationID, invoiceID uuid.UUID, inv *Invoice) error {
	if f.markSentWithSnapshotErr != nil {
		return f.markSentWithSnapshotErr
	}
	existing, ok := f.invoices[invoiceID]
	if !ok || existing.OrganisationID != organisationID {
		return ErrInvoiceNotFound
	}
	updated := *inv
	f.invoices[invoiceID] = updated
	return nil
}

// fakeTx is a minimal stand-in for a pgx.Tx. Only Commit and Rollback are
// ever exercised in this package's tests — fakeInvoiceRepository.WithTx
// ignores the tx value it's given rather than issuing queries through it —
// so every other method exists solely to satisfy the pgx.Tx interface and
// is never called.
type fakeTx struct {
	committed  bool
	rolledBack bool
	commitErr  error
}

func (t *fakeTx) Commit(ctx context.Context) error {
	t.committed = true
	return t.commitErr
}

// Rollback mirrors real pgx.Tx behaviour: once Commit has closed the
// transaction, a later Rollback (e.g. from a deferred call) is a no-op
// that returns pgx.ErrTxClosed rather than actually rolling anything back.
// Without this, a deferred Rollback that always fires after a successful
// Commit would be indistinguishable, from this fake's perspective, from a
// real rollback.
func (t *fakeTx) Rollback(ctx context.Context) error {
	if t.committed {
		return pgx.ErrTxClosed
	}
	t.rolledBack = true
	return nil
}

func (t *fakeTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented in fakeTx")
}
func (t *fakeTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *fakeTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (t *fakeTx) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }
func (t *fakeTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *fakeTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (t *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (t *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (t *fakeTx) Conn() *pgx.Conn                                               { return nil }

// fakeTxBeginner is a minimal in-memory TxBeginner handing back a single
// fakeTx, so tests can inspect whether Commit or Rollback was ultimately
// called.
type fakeTxBeginner struct {
	tx       *fakeTx
	beginErr error
}

func (f *fakeTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

// fakeCustomerRepository is a minimal in-memory customer.CustomerRepository
// used only to check organisation-scoped existence, mirroring
// PostgresCustomerRepository.GetByID's scoping behaviour.
// getByIDCallCount lets PDF service tests (Milestone 7 Part 3) prove an
// issued invoice's PDF never re-reads the live customer record.
type fakeCustomerRepository struct {
	customers map[uuid.UUID]customer.Customer

	getByIDCallCount int
}

func newFakeCustomerRepository() *fakeCustomerRepository {
	return &fakeCustomerRepository{customers: make(map[uuid.UUID]customer.Customer)}
}

func (f *fakeCustomerRepository) add(organisationID uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.customers[id] = customer.Customer{ID: id, OrganisationID: organisationID, Name: "Test Customer"}
	return id
}

// WithTx ignores its tx argument and returns the same fake — it has no
// real transactional semantics of its own, matching every other fake
// repository in this file.
func (f *fakeCustomerRepository) WithTx(tx pgx.Tx) customer.CustomerRepository {
	return f
}

func (f *fakeCustomerRepository) Create(ctx context.Context, c *customer.Customer) error {
	f.customers[c.ID] = *c
	return nil
}

func (f *fakeCustomerRepository) GetByID(ctx context.Context, organisationID, customerID uuid.UUID) (*customer.Customer, error) {
	f.getByIDCallCount++
	c, ok := f.customers[customerID]
	if !ok || c.OrganisationID != organisationID {
		return nil, customer.ErrCustomerNotFound
	}
	return &c, nil
}

// fakeOrganisationRepository is a minimal in-memory admin.OrganisationRepository
// used only to supply Organisation snapshot source data during Send
// (Milestone 7 Part 2). getByIDCallCount lets tests prove a repeated Send
// never re-reads the organisation at all (see
// TestInvoiceService_Send_RepeatedSendNeverReloadsSnapshotSources).
type fakeOrganisationRepository struct {
	organisations map[uuid.UUID]admin.Organisation

	getByIDCallCount int
}

func newFakeOrganisationRepository() *fakeOrganisationRepository {
	return &fakeOrganisationRepository{organisations: make(map[uuid.UUID]admin.Organisation)}
}

func (f *fakeOrganisationRepository) WithTx(tx pgx.Tx) admin.OrganisationRepository {
	return f
}

func (f *fakeOrganisationRepository) Create(ctx context.Context, o *admin.Organisation) error {
	f.organisations[o.ID] = *o
	return nil
}

func (f *fakeOrganisationRepository) GetByID(ctx context.Context, id uuid.UUID) (*admin.Organisation, error) {
	f.getByIDCallCount++
	o, ok := f.organisations[id]
	if !ok {
		return nil, admin.ErrOrganisationNotFound
	}
	return &o, nil
}

func (f *fakeOrganisationRepository) Update(ctx context.Context, organisationID uuid.UUID, o *admin.Organisation) error {
	if _, ok := f.organisations[organisationID]; !ok {
		return admin.ErrOrganisationNotFound
	}
	f.organisations[organisationID] = *o
	return nil
}

// fakeAddressRepository is a minimal in-memory customer.AddressRepository
// used only to supply a customer's billing-address snapshot source data
// during Send (Milestone 7 Part 2). Addresses are keyed by customerID
// directly (not customerID+organisationID) since these tests only ever
// need one organisation's view; getCallCount lets tests prove a repeated
// Send never re-reads the billing address at all.
type fakeAddressRepository struct {
	addresses map[uuid.UUID]customer.Address

	getCallCount int
}

func newFakeAddressRepository() *fakeAddressRepository {
	return &fakeAddressRepository{addresses: make(map[uuid.UUID]customer.Address)}
}

func (f *fakeAddressRepository) WithTx(tx pgx.Tx) customer.AddressRepository {
	return f
}

func (f *fakeAddressRepository) GetBillingAddressByCustomerID(ctx context.Context, organisationID, customerID uuid.UUID) (*customer.Address, error) {
	f.getCallCount++
	a, ok := f.addresses[customerID]
	if !ok {
		return nil, customer.ErrBillingAddressNotFound
	}
	return &a, nil
}

func (f *fakeAddressRepository) UpsertBillingAddress(ctx context.Context, organisationID, customerID uuid.UUID, address *customer.Address) (*customer.Address, error) {
	f.addresses[customerID] = *address
	stored := *address
	return &stored, nil
}

func (f *fakeAddressRepository) add(customerID uuid.UUID, address customer.Address) {
	f.addresses[customerID] = address
}

// fakeProductRepository is a minimal in-memory product.ProductRepository
// used only to check organisation-scoped existence, mirroring
// PostgresProductRepository.GetByID's scoping behaviour.
type fakeProductRepository struct {
	products map[uuid.UUID]product.Product
}

func newFakeProductRepository() *fakeProductRepository {
	return &fakeProductRepository{products: make(map[uuid.UUID]product.Product)}
}

func (f *fakeProductRepository) add(organisationID uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.products[id] = product.Product{ID: id, OrganisationID: organisationID, Name: "Test Product", SKU: "SKU-1"}
	return id
}

func (f *fakeProductRepository) Create(ctx context.Context, p *product.Product) error {
	f.products[p.ID] = *p
	return nil
}

func (f *fakeProductRepository) GetByID(ctx context.Context, organisationID, productID uuid.UUID) (*product.Product, error) {
	p, ok := f.products[productID]
	if !ok || p.OrganisationID != organisationID {
		return nil, product.ErrProductNotFound
	}
	return &p, nil
}

// fakeSettingsRepository is a minimal in-memory admin.SettingsRepository
// used to test the service's invoice-number-allocation orchestration
// without touching PostgreSQL. Like fakeInvoiceRepository, WithTx ignores
// its tx argument and returns the same fake, so it has no real
// transactional/rollback semantics of its own — that guarantee (that a
// rolled-back transaction leaves the allocated number un-consumed) is
// proven separately against real PostgreSQL in invoice_transaction_test.go.
type fakeSettingsRepository struct {
	settings map[uuid.UUID]admin.Settings

	getForUpdateErr        error
	updateInvoiceNumberErr error

	getByOrganisationIDCallCount int
}

func newFakeSettingsRepository() *fakeSettingsRepository {
	return &fakeSettingsRepository{settings: make(map[uuid.UUID]admin.Settings)}
}

func (f *fakeSettingsRepository) add(organisationID uuid.UUID) {
	f.settings[organisationID] = admin.Settings{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		InvoicePrefix:  "INV-",
		InvoiceNumber:  0,
		Currency:       "GBP",
		PaymentTerms:   30,
	}
}

func (f *fakeSettingsRepository) WithTx(tx pgx.Tx) admin.SettingsRepository {
	return f
}

func (f *fakeSettingsRepository) Create(ctx context.Context, settings *admin.Settings) error {
	f.settings[settings.OrganisationID] = *settings
	return nil
}

func (f *fakeSettingsRepository) GetByOrganisationID(ctx context.Context, organisationID uuid.UUID) (*admin.Settings, error) {
	f.getByOrganisationIDCallCount++
	return f.get(organisationID)
}

func (f *fakeSettingsRepository) GetForUpdate(ctx context.Context, organisationID uuid.UUID) (*admin.Settings, error) {
	if f.getForUpdateErr != nil {
		return nil, f.getForUpdateErr
	}
	return f.get(organisationID)
}

func (f *fakeSettingsRepository) get(organisationID uuid.UUID) (*admin.Settings, error) {
	s, ok := f.settings[organisationID]
	if !ok {
		return nil, admin.ErrSettingsNotFound
	}
	return &s, nil
}

func (f *fakeSettingsRepository) UpdateInvoiceNumber(ctx context.Context, organisationID uuid.UUID, invoiceNumber int) error {
	if f.updateInvoiceNumberErr != nil {
		return f.updateInvoiceNumberErr
	}
	s, ok := f.settings[organisationID]
	if !ok {
		return admin.ErrSettingsNotFound
	}
	s.InvoiceNumber = invoiceNumber
	f.settings[organisationID] = s
	return nil
}

// fakePaymentRepository is a minimal in-memory PaymentRepository used to
// test the service's payment orchestration without touching PostgreSQL.
// Like fakeInvoiceRepository, WithTx ignores its tx argument and returns
// the same fake; the real atomicity/locking guarantees are proven
// separately against PostgreSQL in invoice_transaction_test.go.
type fakePaymentRepository struct {
	payments map[uuid.UUID][]*Payment

	createErr       error
	getTotalPaidErr error
}

func newFakePaymentRepository() *fakePaymentRepository {
	return &fakePaymentRepository{payments: make(map[uuid.UUID][]*Payment)}
}

func (f *fakePaymentRepository) WithTx(tx pgx.Tx) PaymentRepository {
	return f
}

// Create, like every other method here, accepts organisationID to match
// PaymentRepository's real (Milestone 4 Part 6) signature, but doesn't
// itself validate it — this fake has no reference to invoice ownership
// data at all (unlike fakeInvoiceRepository, it only ever sees payments
// keyed by invoiceID). It has no real tenant-scoping semantics of its
// own, the same "fakes aren't the real thing" caveat already documented
// on WithTx elsewhere in this file; the actual repository-level
// enforcement is proven separately against real PostgreSQL in
// payment_repository_postgres_test.go. Tests relying on cross-tenant
// rejection go through InvoiceService, which never reaches this fake for
// a foreign invoice because GetForUpdate rejects it first.
func (f *fakePaymentRepository) Create(ctx context.Context, organisationID uuid.UUID, payment *Payment) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.payments[payment.InvoiceID] = append(f.payments[payment.InvoiceID], payment)
	return nil
}

func (f *fakePaymentRepository) GetByInvoiceID(ctx context.Context, organisationID, invoiceID uuid.UUID) ([]*Payment, error) {
	return f.payments[invoiceID], nil
}

func (f *fakePaymentRepository) GetTotalPaidByInvoiceID(ctx context.Context, organisationID, invoiceID uuid.UUID) (int64, error) {
	if f.getTotalPaidErr != nil {
		return 0, f.getTotalPaidErr
	}
	var total int64
	for _, p := range f.payments[invoiceID] {
		total += p.Amount
	}
	return total, nil
}

// testFixture bundles a service with fakes pre-seeded with a valid
// organisation, customer, product and settings row under one
// organisation, for tests that need a happy-path reference to build
// requests around. The default organisation has Name set (and the
// default customer has Name set — see fakeCustomerRepository.add), so
// f.service.Send succeeds out of the box (Milestone 7 Part 2: a
// successful Send needs a resolvable seller name, customer name and
// currency); tests exercising a missing/invalid snapshot field mutate
// organisationRepository/addressRepository/settingsRepository directly.
// repository, paymentRepository, settingsRepository, organisationRepository,
// addressRepository and tx are exposed so individual tests can inject a
// persistence failure, seed an invoice directly, or inspect whether
// Commit/Rollback was called.
type testFixture struct {
	service                *InvoiceService
	repository             *fakeInvoiceRepository
	paymentRepository      *fakePaymentRepository
	settingsRepository     *fakeSettingsRepository
	organisationRepository *fakeOrganisationRepository
	addressRepository      *fakeAddressRepository
	customerRepository     *fakeCustomerRepository
	tx                     *fakeTx
	organisationID         uuid.UUID
	customerID             uuid.UUID
	productID              uuid.UUID
}

func newTestFixture() *testFixture {
	organisationID := uuid.New()

	customers := newFakeCustomerRepository()
	products := newFakeProductRepository()
	customerID := customers.add(organisationID)
	productID := products.add(organisationID)

	repository := newFakeInvoiceRepository()
	paymentRepository := newFakePaymentRepository()
	settingsRepository := newFakeSettingsRepository()
	settingsRepository.add(organisationID)

	organisations := newFakeOrganisationRepository()
	organisations.organisations[organisationID] = admin.Organisation{ID: organisationID, Name: "Test Organisation"}

	addresses := newFakeAddressRepository()
	// No billing address by default — Send must succeed without one
	// (Milestone 7 Part 2); tests that need one call addresses.add(...).

	tx := &fakeTx{}
	txBeginner := &fakeTxBeginner{tx: tx}

	service := NewInvoiceService(repository, customers, products, organisations, addresses, settingsRepository, paymentRepository, txBeginner)

	return &testFixture{
		service:                service,
		repository:             repository,
		paymentRepository:      paymentRepository,
		settingsRepository:     settingsRepository,
		organisationRepository: organisations,
		addressRepository:      addresses,
		customerRepository:     customers,
		tx:                     tx,
		organisationID:         organisationID,
		customerID:             customerID,
		productID:              productID,
	}
}

// addInvoice seeds the fixture's fake invoice repository directly with a
// fully-formed invoice, for payment tests that need an existing invoice
// with a known Total/Status rather than one built through the whole
// invoice-creation flow.
func (f *testFixture) addInvoice(total int64, status string) uuid.UUID {
	inv := Invoice{
		ID:             uuid.New(),
		OrganisationID: f.organisationID,
		CustomerID:     f.customerID,
		InvoiceNumber:  "INV-TEST-" + uuid.New().String(),
		Total:          total,
		Status:         status,
		// 30 days out: comfortably in the future, so a Sent invoice
		// built by this helper never accidentally derives as Overdue via
		// Invoice.EffectiveStatus (Milestone 5) merely because it was
		// never given a DueDate. Tests that specifically need an overdue
		// or boundary DueDate set one explicitly afterwards.
		DueDate: time.Now().UTC().AddDate(0, 0, 30),
	}
	f.repository.invoices[inv.ID] = inv
	return inv.ID
}

// pdfService builds an InvoicePDFService wired against this fixture's
// same fakes — reusing them, rather than a separate set, is what lets
// PDF tests (Milestone 7 Part 3) directly inspect e.g.
// f.organisationRepository.getByIDCallCount to prove an issued invoice's
// PDF never re-reads live data.
func (f *testFixture) pdfService() *InvoicePDFService {
	return NewInvoicePDFService(
		f.repository,
		f.paymentRepository,
		f.organisationRepository,
		f.customerRepository,
		f.addressRepository,
		f.settingsRepository,
		NewInvoicePDFRenderer(),
	)
}

// addIssuedInvoiceWithSnapshot seeds a Sent (or Paid) invoice directly
// with a fully-populated party snapshot, exactly as InvoiceService.Send
// would have captured one (Milestone 7 Part 2) — for PDF tests that need
// an issued invoice without going through the whole Send flow.
func (f *testFixture) addIssuedInvoiceWithSnapshot(total int64, status string, dueDate time.Time) uuid.UUID {
	inv := Invoice{
		ID:             uuid.New(),
		OrganisationID: f.organisationID,
		CustomerID:     f.customerID,
		InvoiceNumber:  "INV-TEST-" + uuid.New().String(),
		Total:          total,
		Status:         status,
		DueDate:        dueDate,
		SentAt:         timePtr(time.Now().UTC()),

		SellerName:    strPtr("Snapshot Seller Ltd"),
		SellerEmail:   strPtr("snapshot-seller@example.test"),
		SellerAddress: strPtr("1 Snapshot Way"),
		SellerCity:    strPtr("London"),
		SellerCountry: strPtr("GB"),

		CustomerName:       strPtr("Snapshot Customer"),
		CustomerEmail:      strPtr("snapshot-customer@example.test"),
		CustomerAddress:    strPtr("2 Snapshot Street"),
		CustomerCity:       strPtr("Manchester"),
		CustomerPostalCode: strPtr("M1 1AE"),
		CustomerCountry:    strPtr("GB"),

		Currency: strPtr("GBP"),
	}
	f.repository.invoices[inv.ID] = inv
	f.repository.lines[inv.ID] = []*Line{
		{ID: uuid.New(), InvoiceID: inv.ID, Description: "Snapshot line", Quantity: 1, UnitPrice: total, VATRate: 0, VATAmount: 0, Total: total},
	}
	return inv.ID
}

func timePtr(t time.Time) *time.Time { return &t }

func validLineRequest() CreateInvoiceLineRequest {
	return CreateInvoiceLineRequest{
		Description: "Consulting services",
		Quantity:    1,
		UnitPrice:   1000,
		VATRate:     20,
	}
}

func validRequest(customerID uuid.UUID, lines ...CreateInvoiceLineRequest) CreateInvoiceRequest {
	if len(lines) == 0 {
		lines = []CreateInvoiceLineRequest{validLineRequest()}
	}
	return CreateInvoiceRequest{
		CustomerID: customerID.String(),
		IssueDate:  "2026-01-01",
		DueDate:    "2026-01-31",
		Lines:      lines,
	}
}

// --- Validation tests ---

func TestInvoiceService_Create_NoLines(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.Lines = nil

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceNoLines) {
		t.Fatalf("expected ErrInvoiceNoLines, got %v", err)
	}
}

func TestInvoiceService_Create_MissingCustomerID(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.CustomerID = "  "

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceCustomerIDRequired) {
		t.Fatalf("expected ErrInvoiceCustomerIDRequired, got %v", err)
	}
}

func TestInvoiceService_Create_InvalidCustomerID(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.CustomerID = "not-a-uuid"

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceCustomerIDInvalid) {
		t.Fatalf("expected ErrInvoiceCustomerIDInvalid, got %v", err)
	}
}

func TestInvoiceService_Create_CustomerNotFound(t *testing.T) {
	f := newTestFixture()
	request := validRequest(uuid.New()) // a customer ID that doesn't exist

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceCustomerNotFound) {
		t.Fatalf("expected ErrInvoiceCustomerNotFound, got %v", err)
	}
}

func TestInvoiceService_Create_MissingIssueDate(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.IssueDate = ""

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceIssueDateRequired) {
		t.Fatalf("expected ErrInvoiceIssueDateRequired, got %v", err)
	}
}

func TestInvoiceService_Create_InvalidIssueDate(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.IssueDate = "not-a-date"

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceIssueDateInvalid) {
		t.Fatalf("expected ErrInvoiceIssueDateInvalid, got %v", err)
	}
}

func TestInvoiceService_Create_MissingDueDate(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.DueDate = ""

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceDueDateRequired) {
		t.Fatalf("expected ErrInvoiceDueDateRequired, got %v", err)
	}
}

func TestInvoiceService_Create_DueDateBeforeIssueDate(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.IssueDate = "2026-01-31"
	request.DueDate = "2026-01-01"

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceDueDateBeforeIssueDate) {
		t.Fatalf("expected ErrInvoiceDueDateBeforeIssueDate, got %v", err)
	}
}

func TestInvoiceService_Create_DueDateEqualsIssueDate(t *testing.T) {
	// A due date equal to the issue date is allowed — only "before" is
	// rejected.
	f := newTestFixture()
	request := validRequest(f.customerID)
	request.IssueDate = "2026-01-01"
	request.DueDate = "2026-01-01"

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("expected same-day due date to be accepted, got %v", err)
	}
}

func TestInvoiceService_Create_ZeroQuantity(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	line.Quantity = 0
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineQuantityInvalid) {
		t.Fatalf("expected ErrInvoiceLineQuantityInvalid, got %v", err)
	}
}

func TestInvoiceService_Create_NegativeQuantity(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	line.Quantity = -1
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineQuantityInvalid) {
		t.Fatalf("expected ErrInvoiceLineQuantityInvalid, got %v", err)
	}
}

func TestInvoiceService_Create_ValidFractionalQuantity(t *testing.T) {
	// 1.5 must be accepted — quantity is not restricted to integers.
	f := newTestFixture()
	line := validLineRequest()
	line.Quantity = 1.5
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("expected fractional quantity 1.5 to be accepted, got %v", err)
	}
}

func TestInvoiceService_Create_NegativeUnitPrice(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	line.UnitPrice = -1
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineUnitPriceNegative) {
		t.Fatalf("expected ErrInvoiceLineUnitPriceNegative, got %v", err)
	}
}

func TestInvoiceService_Create_NegativeVATRate(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	line.VATRate = -1
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineVATRateNegative) {
		t.Fatalf("expected ErrInvoiceLineVATRateNegative, got %v", err)
	}
}

func TestInvoiceService_Create_EmptyDescription(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	line.Description = "   "
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineDescriptionRequired) {
		t.Fatalf("expected ErrInvoiceLineDescriptionRequired, got %v", err)
	}
}

func TestInvoiceService_Create_DescriptionIsTrimmed(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	line.Description = "  Consulting services  "
	request := validRequest(f.customerID, line)

	_, lines, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if lines[0].Description != "Consulting services" {
		t.Errorf("expected trimmed description %q, got %q", "Consulting services", lines[0].Description)
	}
}

func TestInvoiceService_Create_InvalidProductID(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	badID := "not-a-uuid"
	line.ProductID = &badID
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineProductIDInvalid) {
		t.Fatalf("expected ErrInvoiceLineProductIDInvalid, got %v", err)
	}
}

func TestInvoiceService_Create_ProductNotFound(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	missingID := uuid.New().String()
	line.ProductID = &missingID
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineProductNotFound) {
		t.Fatalf("expected ErrInvoiceLineProductNotFound, got %v", err)
	}
}

// --- Domain behaviour tests ---

func TestInvoiceService_Create_ProductBackedLine(t *testing.T) {
	f := newTestFixture()
	line := validLineRequest()
	productIDStr := f.productID.String()
	line.ProductID = &productIDStr
	request := validRequest(f.customerID, line)

	_, lines, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if lines[0].ProductID == nil || *lines[0].ProductID != f.productID {
		t.Errorf("expected line to reference product %v, got %v", f.productID, lines[0].ProductID)
	}

	// The unit price is the client's explicit value, not read from the
	// product — the product lookup is existence-only.
	if lines[0].UnitPrice != 1000 {
		t.Errorf("expected unit price to be the request's own value 1000, got %d", lines[0].UnitPrice)
	}
}

func TestInvoiceService_Create_CustomLineWithoutProduct(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID) // default line has no ProductID

	_, lines, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if lines[0].ProductID != nil {
		t.Errorf("expected no product on a custom line, got %v", *lines[0].ProductID)
	}
}

func TestInvoiceService_Create_CustomerScopedToOrganisation(t *testing.T) {
	// A customer that exists, but under a different organisation, must be
	// treated as not found.
	f := newTestFixture()
	otherOrgCustomers := newFakeCustomerRepository()
	otherCustomerID := otherOrgCustomers.add(uuid.New()) // different org

	request := validRequest(otherCustomerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceCustomerNotFound) {
		t.Fatalf("expected ErrInvoiceCustomerNotFound for a cross-organisation customer, got %v", err)
	}
}

func TestInvoiceService_Create_ProductScopedToOrganisation(t *testing.T) {
	// A product that exists, but under a different organisation, must be
	// treated as not found.
	f := newTestFixture()
	otherOrgProducts := newFakeProductRepository()
	otherProductID := otherOrgProducts.add(uuid.New()).String() // different org

	line := validLineRequest()
	line.ProductID = &otherProductID
	request := validRequest(f.customerID, line)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)

	if !errors.Is(err, ErrInvoiceLineProductNotFound) {
		t.Fatalf("expected ErrInvoiceLineProductNotFound for a cross-organisation product, got %v", err)
	}
}

// --- Successful creation / calculation-through-the-service tests ---

func TestInvoiceService_Create_Success(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID) // quantity 1, unitPrice 1000, vatRate 20

	inv, lines, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if inv.ID == uuid.Nil {
		t.Error("expected a generated invoice ID")
	}

	if inv.OrganisationID != f.organisationID {
		t.Errorf("expected organisation ID %v, got %v", f.organisationID, inv.OrganisationID)
	}

	if inv.CustomerID != f.customerID {
		t.Errorf("expected customer ID %v, got %v", f.customerID, inv.CustomerID)
	}

	if inv.InvoiceNumber == "" {
		t.Error("expected a generated invoice number")
	}

	if inv.Status != InvoiceStatusDraft {
		t.Errorf("expected status %q, got %q", InvoiceStatusDraft, inv.Status)
	}

	if inv.Subtotal != 1000 || inv.VATTotal != 200 || inv.Total != 1200 {
		t.Errorf("expected subtotal=1000 vatTotal=200 total=1200, got subtotal=%d vatTotal=%d total=%d",
			inv.Subtotal, inv.VATTotal, inv.Total)
	}

	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	if lines[0].InvoiceID != inv.ID {
		t.Errorf("expected line's InvoiceID to be %v, got %v", inv.ID, lines[0].InvoiceID)
	}
}

func TestInvoiceService_Create_MultipleLines(t *testing.T) {
	f := newTestFixture()

	lineA := CreateInvoiceLineRequest{Description: "Line A", Quantity: 2, UnitPrice: 1000, VATRate: 20}   // subtotal 2000, vat 400, total 2400
	lineB := CreateInvoiceLineRequest{Description: "Line B", Quantity: 1.5, UnitPrice: 1000, VATRate: 20} // subtotal 1500, vat 300, total 1800
	lineC := CreateInvoiceLineRequest{Description: "Line C", Quantity: 3, UnitPrice: 500, VATRate: 0}     // subtotal 1500, vat 0, total 1500

	request := validRequest(f.customerID, lineA, lineB, lineC)

	inv, lines, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	wantSubtotal := int64(2000 + 1500 + 1500)
	wantVATTotal := int64(400 + 300 + 0)
	wantTotal := int64(2400 + 1800 + 1500)

	if inv.Subtotal != wantSubtotal {
		t.Errorf("subtotal: got %d, want %d", inv.Subtotal, wantSubtotal)
	}

	if inv.VATTotal != wantVATTotal {
		t.Errorf("vatTotal: got %d, want %d", inv.VATTotal, wantVATTotal)
	}

	if inv.Total != wantTotal {
		t.Errorf("total: got %d, want %d", inv.Total, wantTotal)
	}

	if inv.Total != inv.Subtotal+inv.VATTotal {
		t.Errorf("total (%d) does not equal subtotal+vatTotal (%d)", inv.Total, inv.Subtotal+inv.VATTotal)
	}
}

func TestInvoiceService_GetByID(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)

	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	inv, lines, amountPaid, err := f.service.GetByID(context.Background(), f.organisationID, created.ID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if inv.ID != created.ID {
		t.Errorf("expected ID %v, got %v", created.ID, inv.ID)
	}

	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	if amountPaid != 0 {
		t.Errorf("expected amountPaid 0 for an invoice with no payments, got %d", amountPaid)
	}
}

// --- Transaction orchestration tests ---
//
// These test that InvoiceService.Create calls Begin/WithTx/Commit/Rollback
// in the right order and with the right outcome, using fakes that don't
// touch PostgreSQL. The actual database-level atomicity guarantee — that a
// failure partway through truly leaves nothing behind — is proven
// separately against real PostgreSQL in invoice_transaction_test.go.

func TestInvoiceService_Create_CommitsOnSuccess(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	if !f.tx.committed {
		t.Error("expected the transaction to be committed")
	}

	if f.tx.rolledBack {
		t.Error("expected the transaction not to be rolled back")
	}
}

func TestInvoiceService_Create_RollsBackOnInvoiceInsertFailure(t *testing.T) {
	f := newTestFixture()
	f.repository.createErr = errors.New("connection reset by peer")
	request := validRequest(f.customerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestInvoiceService_Create_RollsBackOnLineInsertFailure(t *testing.T) {
	f := newTestFixture()
	f.repository.createLinesErr = errors.New("connection reset by peer")
	request := validRequest(f.customerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestInvoiceService_Create_SettingsNotFound(t *testing.T) {
	f := newTestFixture()
	// A customer/product exist under this organisation, but no settings
	// row does — simulating an organisation that predates automatic
	// settings provisioning, or one whose settings row was never created.
	f.settingsRepository = newFakeSettingsRepository()
	// Same package as InvoiceService, so the unexported field can be
	// swapped directly rather than rebuilding the whole fixture.
	f.service.settingsRepository = f.settingsRepository
	request := validRequest(f.customerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if !errors.Is(err, ErrInvoiceSettingsNotFound) {
		t.Fatalf("expected ErrInvoiceSettingsNotFound, got %v", err)
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}
}

func TestInvoiceService_Create_RollsBackOnSettingsUpdateFailure(t *testing.T) {
	f := newTestFixture()
	f.settingsRepository.updateInvoiceNumberErr = errors.New("connection reset by peer")
	request := validRequest(f.customerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}

	// The repository's Create/CreateLines must never even be attempted
	// once settings allocation has failed.
	if len(f.repository.invoices) != 0 {
		t.Error("expected no invoice to have been created")
	}
}

func TestInvoiceService_Create_BeginError(t *testing.T) {
	f := newTestFixture()
	// Same package as InvoiceService, so the unexported field can be
	// swapped directly rather than rebuilding the whole fixture.
	f.service.txBeginner = &fakeTxBeginner{beginErr: errors.New("pool exhausted")}

	request := validRequest(f.customerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err == nil {
		t.Fatal("expected an error when Begin fails, got nil")
	}
}

func TestInvoiceService_Create_CommitError(t *testing.T) {
	f := newTestFixture()
	f.tx.commitErr = errors.New("commit failed")
	request := validRequest(f.customerID)

	_, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err == nil {
		t.Fatal("expected an error when Commit fails, got nil")
	}

	if !f.tx.committed {
		t.Error("expected Commit to have been called (and to have failed)")
	}
}

func TestInvoiceService_GetByID_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	request := validRequest(f.customerID)

	created, _, err := f.service.Create(context.Background(), f.organisationID, request)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	_, _, _, err = f.service.GetByID(context.Background(), uuid.New(), created.ID)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound for a cross-organisation lookup, got %v", err)
	}
}

// --- GetByID amountPaid/amountOutstanding ---

func TestInvoiceService_GetByID_NoPayments(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	inv, _, amountPaid, err := f.service.GetByID(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if amountPaid != 0 {
		t.Errorf("expected amountPaid 0, got %d", amountPaid)
	}

	if inv.Total-amountPaid != inv.Total {
		t.Errorf("expected outstanding to equal total when nothing has been paid")
	}
}

func TestInvoiceService_GetByID_OnePayment(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	if _, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, CreatePaymentRequest{
		Amount:        4000,
		PaymentMethod: "cash",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	_, _, amountPaid, err := f.service.GetByID(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if amountPaid != 4000 {
		t.Errorf("expected amountPaid 4000, got %d", amountPaid)
	}
}

func TestInvoiceService_GetByID_MultiplePayments_SumsCorrectly(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	ctx := context.Background()

	for _, amount := range []int64{2000, 3000, 1000} {
		if _, _, err := f.service.CreatePayment(ctx, f.organisationID, invoiceID, CreatePaymentRequest{
			Amount:        amount,
			PaymentMethod: "cash",
		}); err != nil {
			t.Fatalf("create payment of %d: %v", amount, err)
		}
	}

	inv, _, amountPaid, err := f.service.GetByID(ctx, f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	wantPaid := int64(2000 + 3000 + 1000)
	if amountPaid != wantPaid {
		t.Errorf("expected amountPaid %d, got %d", wantPaid, amountPaid)
	}

	wantOutstanding := inv.Total - wantPaid
	if inv.Total-amountPaid != wantOutstanding {
		t.Errorf("expected outstanding %d, got %d", wantOutstanding, inv.Total-amountPaid)
	}
}

func TestInvoiceService_GetByID_FullyPaid_OutstandingIsZero(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	if _, _, err := f.service.CreatePayment(context.Background(), f.organisationID, invoiceID, CreatePaymentRequest{
		Amount:        10000,
		PaymentMethod: "cash",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	inv, _, amountPaid, err := f.service.GetByID(context.Background(), f.organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}

	if amountPaid != 10000 {
		t.Errorf("expected amountPaid 10000, got %d", amountPaid)
	}

	if inv.Total-amountPaid != 0 {
		t.Errorf("expected outstanding 0 for a fully paid invoice, got %d", inv.Total-amountPaid)
	}
}

func TestInvoiceService_GetByID_PaymentRepositoryErrorPropagates(t *testing.T) {
	f := newTestFixture()
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	f.paymentRepository.getTotalPaidErr = errors.New("connection reset by peer")

	_, _, _, err := f.service.GetByID(context.Background(), f.organisationID, invoiceID)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !errors.Is(err, f.paymentRepository.getTotalPaidErr) {
		t.Errorf("expected the payment repository's error to propagate unchanged, got %v", err)
	}
}
