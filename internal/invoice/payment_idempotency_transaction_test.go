package invoice

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Milestone 13 Part 1: idempotent payment creation against real
// PostgreSQL — persistence, replay/conflict semantics, tenant scoping,
// the schema's constraints, real concurrency, and ambiguous commits.

// idempotencyTestInvoice creates an organisation, customer and Sent
// invoice of the given total, returning the organisation and invoice IDs.
func idempotencyTestInvoice(t *testing.T, db *pgxpool.Pool, total int64) (uuid.UUID, uuid.UUID) {
	t.Helper()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)

	return organisationID, createTestInvoiceWithTotal(t, db, organisationID, customerID, total, InvoiceStatusSent)
}

func keyedPaymentRequest(key string, amount int64) CreatePaymentRequest {
	return CreatePaymentRequest{
		IdempotencyKey: key,
		Amount:         amount,
		PaymentMethod:  "bank_transfer",
		PaymentDate:    time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
		Reference:      "TX-90210",
		Notes:          "idempotency test",
	}
}

type storedPaymentRow struct {
	id          uuid.UUID
	amount      int64
	key         *string
	requestHash []byte
}

// paymentRows reads every payment row for invoiceID straight from the
// table — independent of any repository method under test.
func paymentRows(t *testing.T, db *pgxpool.Pool, invoiceID uuid.UUID) []storedPaymentRow {
	t.Helper()

	rows, err := db.Query(context.Background(),
		"SELECT id, amount, idempotency_key, request_hash FROM payments WHERE invoice_id = $1 ORDER BY created_at", invoiceID)
	if err != nil {
		t.Fatalf("query payments: %v", err)
	}
	defer rows.Close()

	var result []storedPaymentRow
	for rows.Next() {
		var r storedPaymentRow
		if err := rows.Scan(&r.id, &r.amount, &r.key, &r.requestHash); err != nil {
			t.Fatalf("scan payment: %v", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read payments: %v", err)
	}

	return result
}

func invoiceStatus(t *testing.T, db *pgxpool.Pool, invoiceID uuid.UUID) string {
	t.Helper()

	var status string
	if err := db.QueryRow(context.Background(), "SELECT status FROM invoices WHERE id = $1", invoiceID).Scan(&status); err != nil {
		t.Fatalf("read invoice status: %v", err)
	}

	return status
}

func totalPaid(t *testing.T, db *pgxpool.Pool, invoiceID uuid.UUID) int64 {
	t.Helper()

	var total int64
	if err := db.QueryRow(context.Background(), "SELECT COALESCE(SUM(amount), 0) FROM payments WHERE invoice_id = $1", invoiceID).Scan(&total); err != nil {
		t.Fatalf("sum payments: %v", err)
	}

	return total
}

// --- Creation, persistence, sequential replay ---

func TestIdempotentPayment_FirstRequestCreatesAndStoresKeyAndHash(t *testing.T) {
	db := newTestPool(t)
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 4000)

	result, err := service.CreatePayment(context.Background(), organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if result.Replayed {
		t.Error("expected the first request not to be a replay")
	}

	rows := paymentRows(t, db, invoiceID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 payment row, got %d", len(rows))
	}
	if rows[0].id != result.Payment.ID {
		t.Errorf("expected stored payment %s, got %s", result.Payment.ID, rows[0].id)
	}
	if rows[0].key == nil || *rows[0].key != request.IdempotencyKey {
		t.Errorf("expected stored idempotency key %q, got %v", request.IdempotencyKey, rows[0].key)
	}
	if !bytes.Equal(rows[0].requestHash, fingerprintPaymentRequest(request)) {
		t.Error("expected the stored request_hash to be the request's fingerprint")
	}
}

// Covers a partial payment: a sequential identical retry replays the same
// payment and records nothing further — the lost-response duplicate the
// Milestone 13 Part 1 audit confirmed.
func TestIdempotentPayment_SequentialPartialRetryReplaysWithoutDuplicate(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 4000)

	first, err := service.CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	for i := 0; i < 3; i++ {
		retry, err := service.CreatePayment(ctx, organisationID, invoiceID, request)
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		if !retry.Replayed {
			t.Errorf("retry %d: expected a replay", i)
		}
		if retry.Payment.ID != first.Payment.ID {
			t.Errorf("retry %d: expected payment %s, got %s", i, first.Payment.ID, retry.Payment.ID)
		}
		if retry.Payment.Amount != 4000 || !retry.Payment.CreatedAt.Equal(first.Payment.CreatedAt) {
			t.Errorf("retry %d: expected the replay to reconstruct the original payment", i)
		}
	}

	if got := len(paymentRows(t, db, invoiceID)); got != 1 {
		t.Errorf("expected exactly 1 payment row, got %d", got)
	}
	if got := totalPaid(t, db, invoiceID); got != 4000 {
		t.Errorf("expected total paid 4000, got %d", got)
	}
	if got := invoiceStatus(t, db, invoiceID); got != InvoiceStatusSent {
		t.Errorf("expected the invoice to remain %q, got %q", InvoiceStatusSent, got)
	}
}

// The key ordering requirement against real PostgreSQL: retrying the
// payment that settled the invoice replays it (201), not
// ErrInvoiceCannotAcceptPayment because the invoice is now Paid.
func TestIdempotentPayment_FullPaymentRetryReplaysInsteadOfLifecycleConflict(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 10000)

	first, err := service.CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	if got := invoiceStatus(t, db, invoiceID); got != InvoiceStatusPaid {
		t.Fatalf("expected the invoice to be Paid, got %q", got)
	}

	retry, err := service.CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("expected the retry to replay, got %v", err)
	}
	if !retry.Replayed || retry.Payment.ID != first.Payment.ID {
		t.Errorf("expected a replay of %s, got replayed=%v id=%s", first.Payment.ID, retry.Replayed, retry.Payment.ID)
	}

	// A genuinely new payment (new key) is still refused by the lifecycle.
	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, keyedPaymentRequest(newTestIdempotencyKey(), 1)); !errors.Is(err, ErrInvoiceCannotAcceptPayment) {
		t.Errorf("expected a new payment against the Paid invoice to be refused, got %v", err)
	}

	if got := len(paymentRows(t, db, invoiceID)); got != 1 {
		t.Errorf("expected exactly 1 payment row, got %d", got)
	}
	if got := invoiceStatus(t, db, invoiceID); got != InvoiceStatusPaid {
		t.Errorf("expected the invoice to remain Paid, got %q", got)
	}
}

// An omitted payment date fingerprints as omitted, so a retry after the
// original's "today" has passed still replays. Simulated by moving the
// stored payment_date back a day, exactly as if the original had been
// recorded yesterday.
func TestIdempotentPayment_OmittedDateRetryAcrossMidnightReplays(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 2500)
	request.PaymentDate = time.Time{}

	first, err := service.CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	if _, err := db.Exec(ctx, "UPDATE payments SET payment_date = payment_date - 1 WHERE id = $1", first.Payment.ID); err != nil {
		t.Fatalf("simulate yesterday: %v", err)
	}

	retry, err := service.CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !retry.Replayed || retry.Payment.ID != first.Payment.ID {
		t.Fatalf("expected a replay of %s, got replayed=%v id=%s", first.Payment.ID, retry.Replayed, retry.Payment.ID)
	}
	if want := first.Payment.PaymentDate.AddDate(0, 0, -1).Format(dateLayout); retry.Payment.PaymentDate.Format(dateLayout) != want {
		t.Errorf("expected the replay to carry the stored date %s, got %s", want, retry.Payment.PaymentDate.Format(dateLayout))
	}

	// Explicitly supplying a date is a different logical request.
	explicit := request
	explicit.PaymentDate = time.Now().UTC()
	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, explicit); !errors.Is(err, ErrPaymentIdempotencyKeyReused) {
		t.Errorf("expected an explicit date under the same key to conflict, got %v", err)
	}
}

// --- Conflict ---

func TestIdempotentPayment_SameKeyDifferentPayloadConflictsAndChangesNothing(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	key := newTestIdempotencyKey()

	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, keyedPaymentRequest(key, 3000)); err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	for name, mutate := range map[string]func(*CreatePaymentRequest){
		"amount":    func(r *CreatePaymentRequest) { r.Amount = 7000 },
		"method":    func(r *CreatePaymentRequest) { r.PaymentMethod = "cash" },
		"date":      func(r *CreatePaymentRequest) { r.PaymentDate = r.PaymentDate.AddDate(0, 0, 1) },
		"reference": func(r *CreatePaymentRequest) { r.Reference = "TX-OTHER" },
		"notes":     func(r *CreatePaymentRequest) { r.Notes = "different" },
	} {
		request := keyedPaymentRequest(key, 3000)
		mutate(&request)

		if _, err := service.CreatePayment(ctx, organisationID, invoiceID, request); !errors.Is(err, ErrPaymentIdempotencyKeyReused) {
			t.Errorf("%s changed: expected ErrPaymentIdempotencyKeyReused, got %v", name, err)
		}
	}

	if got := len(paymentRows(t, db, invoiceID)); got != 1 {
		t.Errorf("expected exactly 1 payment row, got %d", got)
	}
	if got := totalPaid(t, db, invoiceID); got != 3000 {
		t.Errorf("expected total paid to remain 3000, got %d", got)
	}
	if got := invoiceStatus(t, db, invoiceID); got != InvoiceStatusSent {
		t.Errorf("expected the invoice to remain %q, got %q", InvoiceStatusSent, got)
	}
}

// Keys are case-sensitive: differing only in case, they identify
// different logical payments.
func TestIdempotentPayment_KeysAreCaseSensitive(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)

	lower, err := service.CreatePayment(ctx, organisationID, invoiceID, keyedPaymentRequest("payment-attempt-abcdef", 1000))
	if err != nil {
		t.Fatalf("lower-case key: %v", err)
	}
	upper, err := service.CreatePayment(ctx, organisationID, invoiceID, keyedPaymentRequest("PAYMENT-ATTEMPT-ABCDEF", 1000))
	if err != nil {
		t.Fatalf("upper-case key: %v", err)
	}

	if upper.Replayed || upper.Payment.ID == lower.Payment.ID {
		t.Error("expected keys differing only in case to create distinct payments")
	}
	if got := len(paymentRows(t, db, invoiceID)); got != 2 {
		t.Errorf("expected 2 payment rows, got %d", got)
	}
}

// --- Scope and tenant isolation ---

// The same key used independently by two tenants, each against its own
// invoice, creates one payment each — neither sees the other's.
func TestIdempotentPayment_SameKeyIndependentAcrossTenants(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationA, invoiceA := idempotencyTestInvoice(t, db, 10000)
	organisationB, invoiceB := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	key := newTestIdempotencyKey()

	a, err := service.CreatePayment(ctx, organisationA, invoiceA, keyedPaymentRequest(key, 1000))
	if err != nil {
		t.Fatalf("tenant A: %v", err)
	}
	// A different payload, too: no conflict, because it's a different scope.
	b, err := service.CreatePayment(ctx, organisationB, invoiceB, keyedPaymentRequest(key, 2000))
	if err != nil {
		t.Fatalf("tenant B: %v", err)
	}

	if a.Replayed || b.Replayed || a.Payment.ID == b.Payment.ID {
		t.Error("expected two independent, newly created payments")
	}
	if len(paymentRows(t, db, invoiceA)) != 1 || len(paymentRows(t, db, invoiceB)) != 1 {
		t.Error("expected exactly one payment per tenant's invoice")
	}
}

// A cross-tenant request is still a 404 (ErrInvoiceNotFound) — even when
// it carries the very key and payload the owning tenant already used, so
// it can neither replay the owner's payment nor learn that the key exists
// (which a conflict would reveal).
func TestIdempotentPayment_CrossTenantStillNotFoundEvenWithOwnersKey(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationA, invoiceA := idempotencyTestInvoice(t, db, 10000)
	organisationB := createTestOrganisation(t, db)
	service := newPaymentTestService(db)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 1000)

	if _, err := service.CreatePayment(ctx, organisationA, invoiceA, request); err != nil {
		t.Fatalf("owner: %v", err)
	}

	for name, attempt := range map[string]CreatePaymentRequest{
		"same key, same payload":      request,
		"same key, different payload": keyedPaymentRequest(request.IdempotencyKey, 9999),
		"new key":                     keyedPaymentRequest(newTestIdempotencyKey(), 1000),
	} {
		if _, err := service.CreatePayment(ctx, organisationB, invoiceA, attempt); !errors.Is(err, ErrInvoiceNotFound) {
			t.Errorf("%s: expected ErrInvoiceNotFound, got %v", name, err)
		}
	}

	if got := len(paymentRows(t, db, invoiceA)); got != 1 {
		t.Errorf("expected exactly 1 payment row, got %d", got)
	}
}

// Documented per-invoice scope: the same key on a different invoice of the
// same organisation is a different logical payment, not a replay or a
// conflict.
func TestIdempotentPayment_SameKeyOnAnotherInvoiceIsIndependent(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceA := idempotencyTestInvoice(t, db, 10000)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceB := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusSent)
	service := newPaymentTestService(db)
	key := newTestIdempotencyKey()

	a, err := service.CreatePayment(ctx, organisationID, invoiceA, keyedPaymentRequest(key, 1000))
	if err != nil {
		t.Fatalf("invoice A: %v", err)
	}
	b, err := service.CreatePayment(ctx, organisationID, invoiceB, keyedPaymentRequest(key, 1000))
	if err != nil {
		t.Fatalf("invoice B: %v", err)
	}

	if b.Replayed || a.Payment.ID == b.Payment.ID {
		t.Error("expected the same key on another invoice to create a separate payment")
	}
	if b.Payment.InvoiceID != invoiceB {
		t.Errorf("expected invoice B's payment to belong to invoice B, got %s", b.Payment.InvoiceID)
	}
}

// --- Failures never consume a key ---

func TestIdempotentPayment_FailedOverpaymentDoesNotConsumeKey(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	key := newTestIdempotencyKey()

	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, keyedPaymentRequest(key, 20000)); !errors.Is(err, ErrPaymentExceedsOutstanding) {
		t.Fatalf("expected ErrPaymentExceedsOutstanding, got %v", err)
	}
	if got := len(paymentRows(t, db, invoiceID)); got != 0 {
		t.Fatalf("expected no payment rows after the rejected overpayment, got %d", got)
	}

	// Same key, corrected amount: executes normally — neither a replay
	// nor a conflict, because the failure stored nothing.
	result, err := service.CreatePayment(ctx, organisationID, invoiceID, keyedPaymentRequest(key, 5000))
	if err != nil {
		t.Fatalf("corrected retry: %v", err)
	}
	if result.Replayed {
		t.Error("expected the corrected retry to create a payment, not replay")
	}
	if got := totalPaid(t, db, invoiceID); got != 5000 {
		t.Errorf("expected total paid 5000, got %d", got)
	}
}

func TestIdempotentPayment_FailedLifecycleRequestDoesNotConsumeKey(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	invoiceID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)
	service := newPaymentTestService(db)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 4000)

	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, request); !errors.Is(err, ErrInvoiceCannotAcceptPayment) {
		t.Fatalf("expected ErrInvoiceCannotAcceptPayment for a Draft invoice, got %v", err)
	}
	if got := len(paymentRows(t, db, invoiceID)); got != 0 {
		t.Fatalf("expected no payment rows, got %d", got)
	}

	if _, err := db.Exec(ctx, "UPDATE invoices SET status = $1 WHERE id = $2", InvoiceStatusSent, invoiceID); err != nil {
		t.Fatalf("mark invoice sent: %v", err)
	}

	result, err := service.CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("retry after the invoice became payable: %v", err)
	}
	if result.Replayed {
		t.Error("expected the retry to create the payment, not replay")
	}
}

// --- Historical rows and schema constraints ---

func insertRawPayment(ctx context.Context, db *pgxpool.Pool, invoiceID uuid.UUID, key any, hash any) error {
	_, err := db.Exec(ctx, `
		INSERT INTO payments (id, invoice_id, amount, payment_method, payment_date, idempotency_key, request_hash)
		VALUES ($1, $2, 100, 'cash', CURRENT_DATE, $3, $4)`,
		uuid.New(), invoiceID, key, hash)
	return err
}

// Payments recorded before migration 000015 have NULL key/hash; they stay
// valid and keep counting towards the invoice's totals alongside new,
// keyed payments.
func TestIdempotentPayment_HistoricalNullKeyPaymentsRemainValid(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)

	for i := 0; i < 2; i++ {
		if err := insertRawPayment(ctx, db, invoiceID, nil, nil); err != nil {
			t.Fatalf("insert historical payment %d: %v", i, err)
		}
	}

	if _, err := service.CreatePayment(ctx, organisationID, invoiceID, keyedPaymentRequest(newTestIdempotencyKey(), 9800)); err != nil {
		t.Fatalf("keyed payment alongside historical ones: %v", err)
	}

	payments, err := service.GetPayments(ctx, organisationID, invoiceID)
	if err != nil {
		t.Fatalf("get payments: %v", err)
	}
	if len(payments) != 3 {
		t.Errorf("expected 3 payments, got %d", len(payments))
	}
	if got := invoiceStatus(t, db, invoiceID); got != InvoiceStatusPaid {
		t.Errorf("expected 100+100+9800 to settle the invoice, got status %q", got)
	}
}

func TestIdempotentPayment_SchemaConstraints(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	_, invoiceID := idempotencyTestInvoice(t, db, 1000000)
	_, otherInvoiceID := idempotencyTestInvoice(t, db, 1000000)
	hash := fingerprintPaymentRequest(CreatePaymentRequest{Amount: 1})

	const (
		checkViolation  = "23514"
		uniqueViolation = "23505"
	)

	expectCode := func(name string, err error, code string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Errorf("%s: expected SQLSTATE %s, got %v", name, code, err)
		}
	}

	expectCode("key without hash", insertRawPayment(ctx, db, invoiceID, "key-without-a-hash-1", nil), checkViolation)
	expectCode("hash without key", insertRawPayment(ctx, db, invoiceID, nil, hash), checkViolation)
	expectCode("15-character key", insertRawPayment(ctx, db, invoiceID, "fifteen-chars-x", hash), checkViolation)
	expectCode("129-character key", insertRawPayment(ctx, db, invoiceID, string(bytes.Repeat([]byte("k"), 129)), hash), checkViolation)
	expectCode("31-byte hash", insertRawPayment(ctx, db, invoiceID, "valid-key-length-1", hash[:31]), checkViolation)
	expectCode("33-byte hash", insertRawPayment(ctx, db, invoiceID, "valid-key-length-2", append(append([]byte{}, hash...), 0)), checkViolation)

	for name, err := range map[string]error{
		"16-character key":        insertRawPayment(ctx, db, invoiceID, "sixteen-chars-xx", hash),
		"128-character key":       insertRawPayment(ctx, db, invoiceID, string(bytes.Repeat([]byte("k"), 128)), hash),
		"NULL pair":               insertRawPayment(ctx, db, invoiceID, nil, nil),
		"second NULL pair":        insertRawPayment(ctx, db, invoiceID, nil, nil),
		"reusable-key":            insertRawPayment(ctx, db, invoiceID, "reusable-key-0001", hash),
		"same key, other invoice": insertRawPayment(ctx, db, otherInvoiceID, "reusable-key-0001", hash),
	} {
		if err != nil {
			t.Errorf("%s: expected the insert to succeed, got %v", name, err)
		}
	}

	expectCode("duplicate (invoice, key)", insertRawPayment(ctx, db, invoiceID, "reusable-key-0001", hash), uniqueViolation)
}

// CreatePayment's concurrent-replay guarantee relies on READ COMMITTED
// (see its doc comment): the key lookup must see a row another
// transaction committed while this one waited on the invoice lock. This
// pins the isolation level a pool-begun transaction actually runs at.
func TestIdempotentPayment_PaymentTransactionsRunAtReadCommitted(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var isolation string
	if err := tx.QueryRow(ctx, "SELECT current_setting('transaction_isolation')").Scan(&isolation); err != nil {
		t.Fatalf("read isolation level: %v", err)
	}

	if isolation != "read committed" {
		t.Fatalf("expected payment transactions to run at READ COMMITTED, got %q", isolation)
	}
}

// --- Concurrency ---

type concurrentPaymentOutcome struct {
	request CreatePaymentRequest
	result  CreatePaymentResult
	err     error
}

// runConcurrentPayments fires every request at once (released together by
// a shared start barrier) and waits for all of them. Nothing here depends
// on timing: every assertion made on the outcomes holds under any
// interleaving the invoice lock allows.
func runConcurrentPayments(service *InvoiceService, organisationID, invoiceID uuid.UUID, requests []CreatePaymentRequest) []concurrentPaymentOutcome {
	outcomes := make([]concurrentPaymentOutcome, len(requests))
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i, request := range requests {
		wg.Add(1)
		go func(i int, request CreatePaymentRequest) {
			defer wg.Done()
			<-start
			result, err := service.CreatePayment(context.Background(), organisationID, invoiceID, request)
			outcomes[i] = concurrentPaymentOutcome{request: request, result: result, err: err}
		}(i, request)
	}

	close(start)
	wg.Wait()

	return outcomes
}

func TestIdempotentPayment_ConcurrentSameKeySamePayloadCreatesExactlyOne(t *testing.T) {
	for _, tc := range []struct {
		name   string
		amount int64
		status string
	}{
		{"partial payment", 4000, InvoiceStatusSent},
		{"full payment", 10000, InvoiceStatusPaid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestPool(t)
			organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
			service := newPaymentTestService(db)
			request := keyedPaymentRequest(newTestIdempotencyKey(), tc.amount)

			const attempts = 10
			requests := make([]CreatePaymentRequest, attempts)
			for i := range requests {
				requests[i] = request
			}

			outcomes := runConcurrentPayments(service, organisationID, invoiceID, requests)

			created, replayed := 0, 0
			paymentIDs := make(map[uuid.UUID]bool)
			for i, o := range outcomes {
				if o.err != nil {
					t.Fatalf("attempt %d: unexpected error: %v", i, o.err)
				}
				if o.result.Replayed {
					replayed++
				} else {
					created++
				}
				paymentIDs[o.result.Payment.ID] = true
			}

			if created != 1 || replayed != attempts-1 {
				t.Errorf("expected 1 creation and %d replays, got %d and %d", attempts-1, created, replayed)
			}
			if len(paymentIDs) != 1 {
				t.Errorf("expected every result to refer to the same payment, got %d distinct IDs", len(paymentIDs))
			}

			rows := paymentRows(t, db, invoiceID)
			if len(rows) != 1 {
				t.Fatalf("expected exactly 1 payment row, got %d", len(rows))
			}
			if !paymentIDs[rows[0].id] {
				t.Error("expected the stored payment to be the one every result refers to")
			}
			if got := totalPaid(t, db, invoiceID); got != tc.amount {
				t.Errorf("expected total paid %d, got %d", tc.amount, got)
			}
			if got := invoiceStatus(t, db, invoiceID); got != tc.status {
				t.Errorf("expected invoice status %q, got %q", tc.status, got)
			}
		})
	}
}

func TestIdempotentPayment_ConcurrentSameKeyDifferentPayloadsCreatesExactlyOne(t *testing.T) {
	db := newTestPool(t)
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	service := newPaymentTestService(db)
	key := newTestIdempotencyKey()

	const attempts = 10
	requests := make([]CreatePaymentRequest, attempts)
	for i := range requests {
		// Two competing payloads under one key, interleaved.
		requests[i] = keyedPaymentRequest(key, 3000+int64(i%2)*1000)
	}

	outcomes := runConcurrentPayments(service, organisationID, invoiceID, requests)

	var winner *concurrentPaymentOutcome
	for i := range outcomes {
		if outcomes[i].err == nil && !outcomes[i].result.Replayed {
			if winner != nil {
				t.Fatal("expected exactly one request to create a payment, got more")
			}
			winner = &outcomes[i]
		}
	}
	if winner == nil {
		t.Fatal("expected exactly one request to create a payment, got none")
	}

	winnerHash := fingerprintPaymentRequest(winner.request)
	for i, o := range outcomes {
		if &outcomes[i] == winner {
			continue
		}

		if bytes.Equal(fingerprintPaymentRequest(o.request), winnerHash) {
			if o.err != nil || !o.result.Replayed || o.result.Payment.ID != winner.result.Payment.ID {
				t.Errorf("attempt %d (winner's payload): expected a replay of %s, got replayed=%v err=%v", i, winner.result.Payment.ID, o.result.Replayed, o.err)
			}
		} else if !errors.Is(o.err, ErrPaymentIdempotencyKeyReused) {
			t.Errorf("attempt %d (other payload): expected ErrPaymentIdempotencyKeyReused, got %v", i, o.err)
		}
	}

	rows := paymentRows(t, db, invoiceID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 payment row, got %d", len(rows))
	}
	if rows[0].id != winner.result.Payment.ID || rows[0].amount != winner.request.Amount {
		t.Error("expected the stored payment to be the winner's")
	}
	if !bytes.Equal(rows[0].requestHash, winnerHash) {
		t.Error("expected the winner's request to define the stored fingerprint")
	}
	if got := totalPaid(t, db, invoiceID); got != winner.request.Amount {
		t.Errorf("expected total paid %d, got %d", winner.request.Amount, got)
	}
	if got := invoiceStatus(t, db, invoiceID); got != InvoiceStatusSent {
		t.Errorf("expected the invoice to remain %q, got %q", InvoiceStatusSent, got)
	}
}

// --- Ambiguous commit ---

// ambiguousCommitTx wraps a real pgx.Tx whose Commit genuinely commits (or,
// with rollbackInstead, genuinely rolls back) and then reports an error
// anyway — the client-visible shape of a connection lost after COMMIT was
// sent, where the application cannot know which outcome occurred.
type ambiguousCommitTx struct {
	pgx.Tx
	rollbackInstead bool
}

var errSimulatedAmbiguousCommit = errors.New("simulated: connection lost after COMMIT was sent")

func (t ambiguousCommitTx) Commit(ctx context.Context) error {
	var err error
	if t.rollbackInstead {
		err = t.Tx.Rollback(ctx)
	} else {
		err = t.Tx.Commit(ctx)
	}
	if err != nil {
		return err
	}

	return errSimulatedAmbiguousCommit
}

type ambiguousCommitBeginner struct {
	db              *pgxpool.Pool
	rollbackInstead bool
}

func (b ambiguousCommitBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, err
	}

	return ambiguousCommitTx{Tx: tx, rollbackInstead: b.rollbackInstead}, nil
}

func TestIdempotentPayment_AmbiguousCommitThatCommittedIsReplayedOnRetry(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 10000)

	ambiguous := newPaymentTestService(db)
	ambiguous.txBeginner = ambiguousCommitBeginner{db: db}

	if _, err := ambiguous.CreatePayment(ctx, organisationID, invoiceID, request); !errors.Is(err, errSimulatedAmbiguousCommit) {
		t.Fatalf("expected the service to report the commit failure, got %v", err)
	}

	rows := paymentRows(t, db, invoiceID)
	if len(rows) != 1 {
		t.Fatalf("expected the payment to have genuinely committed, got %d rows", len(rows))
	}
	if got := invoiceStatus(t, db, invoiceID); got != InvoiceStatusPaid {
		t.Fatalf("expected the Paid transition to have committed with the payment, got %q", got)
	}

	retry, err := newPaymentTestService(db).CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !retry.Replayed || retry.Payment.ID != rows[0].id {
		t.Errorf("expected the retry to replay committed payment %s, got replayed=%v id=%s", rows[0].id, retry.Replayed, retry.Payment.ID)
	}
	if got := len(paymentRows(t, db, invoiceID)); got != 1 {
		t.Errorf("expected exactly 1 payment row after the retry, got %d", got)
	}
}

func TestIdempotentPayment_AmbiguousCommitThatDidNotCommitExecutesOnRetry(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	organisationID, invoiceID := idempotencyTestInvoice(t, db, 10000)
	request := keyedPaymentRequest(newTestIdempotencyKey(), 4000)

	ambiguous := newPaymentTestService(db)
	ambiguous.txBeginner = ambiguousCommitBeginner{db: db, rollbackInstead: true}

	if _, err := ambiguous.CreatePayment(ctx, organisationID, invoiceID, request); !errors.Is(err, errSimulatedAmbiguousCommit) {
		t.Fatalf("expected the service to report the commit failure, got %v", err)
	}
	if got := len(paymentRows(t, db, invoiceID)); got != 0 {
		t.Fatalf("expected nothing durable, got %d rows", got)
	}

	retry, err := newPaymentTestService(db).CreatePayment(ctx, organisationID, invoiceID, request)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retry.Replayed {
		t.Error("expected the retry to execute normally, not replay")
	}
	if got := len(paymentRows(t, db, invoiceID)); got != 1 {
		t.Errorf("expected exactly 1 payment row, got %d", got)
	}
}
