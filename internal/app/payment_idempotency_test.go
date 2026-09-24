package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Milestone 13 Part 1: idempotent payment creation through the real
// router, authentication middleware and PostgreSQL.

// doRequestWithIdempotencyKey is doRequest with explicit control over the
// Idempotency-Key header values (none if keys is empty) — doRequest itself
// sends a fresh key on every payment POST.
func doRequestWithIdempotencyKey(handler http.Handler, path, token, body string, keys ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for _, key := range keys {
		request.Header.Add("Idempotency-Key", key)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

// createSentInvoice creates a customer and a 1000-minor-unit invoice for
// tenant, sends it, and returns the invoice ID.
func createSentInvoice(t *testing.T, handler http.Handler, token string) string {
	t.Helper()

	customerRecorder := doRequest(handler, http.MethodPost, "/api/v1/customers", token, bytes.NewBufferString(`{"name":"Idempotency Customer"}`))
	if customerRecorder.Code != http.StatusCreated {
		t.Fatalf("create customer: status %d (body: %s)", customerRecorder.Code, customerRecorder.Body.String())
	}
	var customer struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(customerRecorder.Body).Decode(&customer)

	invoiceRecorder := doRequest(handler, http.MethodPost, "/api/v1/invoices", token, bytes.NewBufferString(`{
		"customerId": "`+customer.ID+`",
		"issueDate": "2026-01-01",
		"dueDate": "`+time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")+`",
		"lines": [{"description": "Consulting", "quantity": 1, "unitPrice": 1000, "vatRate": 0}]
	}`))
	if invoiceRecorder.Code != http.StatusCreated {
		t.Fatalf("create invoice: status %d (body: %s)", invoiceRecorder.Code, invoiceRecorder.Body.String())
	}
	var invoice struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(invoiceRecorder.Body).Decode(&invoice)

	if recorder := doRequest(handler, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/send", token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("send invoice: status %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	return invoice.ID
}

func paymentCount(t *testing.T, handler http.Handler, token, invoiceID string) int {
	t.Helper()

	recorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoiceID+"/payments", token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list payments: status %d (body: %s)", recorder.Code, recorder.Body.String())
	}
	var payments []json.RawMessage
	if err := json.NewDecoder(recorder.Body).Decode(&payments); err != nil {
		t.Fatalf("decode payments: %v", err)
	}

	return len(payments)
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (body: %s)", err, recorder.Body.String())
	}

	return body.Error.Code
}

func TestApp_PaymentIdempotency_EndToEnd(t *testing.T) {
	handler, db := newTestApp(t)
	tenant := registerTenant(t, handler, db, "Idempotency Org", "idempotency-admin-"+uuid.NewString()+"@example.com")
	invoiceID := createSentInvoice(t, handler, tenant.token)
	path := "/api/v1/invoices/" + invoiceID + "/payments"
	partial := `{"amount":400,"paymentMethod":"cash","paymentDate":"2026-01-15"}`

	// Key required.
	if recorder := doRequestWithIdempotencyKey(handler, path, tenant.token, partial); recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing key: expected %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}

	// Partial payment, then a retry: one payment, identical 201 bodies.
	partialKey := uuid.NewString()
	first := doRequestWithIdempotencyKey(handler, path, tenant.token, partial, partialKey)
	if first.Code != http.StatusCreated {
		t.Fatalf("partial payment: expected %d, got %d (body: %s)", http.StatusCreated, first.Code, first.Body.String())
	}
	retry := doRequestWithIdempotencyKey(handler, path, tenant.token, partial, partialKey)
	if retry.Code != http.StatusCreated || retry.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("partial retry: expected a 201 replay, got %d replayed=%q (body: %s)", retry.Code, retry.Header().Get("Idempotent-Replayed"), retry.Body.String())
	}
	if first.Body.String() != retry.Body.String() {
		t.Errorf("expected identical bodies:\n%s\n%s", first.Body.String(), retry.Body.String())
	}
	if got := paymentCount(t, handler, tenant.token, invoiceID); got != 1 {
		t.Fatalf("expected 1 payment after the retry, got %d", got)
	}

	// Same key, different payload: 409 idempotency_key_reused, nothing recorded.
	conflict := doRequestWithIdempotencyKey(handler, path, tenant.token, `{"amount":500,"paymentMethod":"cash","paymentDate":"2026-01-15"}`, partialKey)
	if conflict.Code != http.StatusConflict || errorCode(t, conflict) != "idempotency_key_reused" {
		t.Fatalf("expected 409 idempotency_key_reused, got %d (body: %s)", conflict.Code, conflict.Body.String())
	}

	// Final payment settles the invoice; retrying it replays rather than
	// hitting the Paid lifecycle rule.
	final := `{"amount":600,"paymentMethod":"cash","paymentDate":"2026-01-20"}`
	finalKey := uuid.NewString()
	if recorder := doRequestWithIdempotencyKey(handler, path, tenant.token, final, finalKey); recorder.Code != http.StatusCreated {
		t.Fatalf("final payment: expected %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}
	if recorder := doRequestWithIdempotencyKey(handler, path, tenant.token, final, finalKey); recorder.Code != http.StatusCreated || recorder.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("final retry: expected a 201 replay, got %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	invoiceRecorder := doRequest(handler, http.MethodGet, "/api/v1/invoices/"+invoiceID, tenant.token, nil)
	var inv struct {
		Status     string `json:"status"`
		AmountPaid int64  `json:"amountPaid"`
	}
	if err := json.NewDecoder(invoiceRecorder.Body).Decode(&inv); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	if inv.Status != "paid" || inv.AmountPaid != 1000 {
		t.Errorf("expected a paid invoice with amountPaid 1000, got status %q amountPaid %d", inv.Status, inv.AmountPaid)
	}
	if got := paymentCount(t, handler, tenant.token, invoiceID); got != 2 {
		t.Errorf("expected exactly 2 payments, got %d", got)
	}
}

// The real AuthMiddleware runs before the handler ever reads the key.
func TestApp_PaymentIdempotency_AuthenticationPrecedesKeyValidation(t *testing.T) {
	handler, _ := newTestApp(t)
	path := "/api/v1/invoices/" + uuid.NewString() + "/payments"
	body := `{"amount":100,"paymentMethod":"cash"}`

	for name, keys := range map[string][]string{"no key": nil, "malformed key": {"x"}, "valid key": {uuid.NewString()}} {
		if recorder := doRequestWithIdempotencyKey(handler, path, "", body, keys...); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected %d, got %d", name, http.StatusUnauthorized, recorder.Code)
		}
	}
}

// A key is not bound to the session that first used it: after logging
// in again (e.g. the original session expired mid-retry), the same key
// still replays the payment.
func TestApp_PaymentIdempotency_ReplaysAcrossSessions(t *testing.T) {
	handler, db := newTestApp(t)
	email := "idempotency-session-" + uuid.NewString() + "@example.com"
	tenant := registerTenant(t, handler, db, "Idempotency Session Org", email)
	invoiceID := createSentInvoice(t, handler, tenant.token)
	path := "/api/v1/invoices/" + invoiceID + "/payments"
	body := `{"amount":250,"paymentMethod":"cash","paymentDate":"2026-01-15"}`
	key := uuid.NewString()

	first := doRequestWithIdempotencyKey(handler, path, tenant.token, body, key)
	if first.Code != http.StatusCreated {
		t.Fatalf("first attempt: expected %d, got %d", http.StatusCreated, first.Code)
	}

	secondSession := loginAs(t, handler, email, testPassword)
	retry := doRequestWithIdempotencyKey(handler, path, secondSession, body, key)
	if retry.Code != http.StatusCreated || retry.Header().Get("Idempotent-Replayed") != "true" || retry.Body.String() != first.Body.String() {
		t.Fatalf("expected a 201 replay from the new session, got %d (body: %s)", retry.Code, retry.Body.String())
	}
}

// Another tenant sending the owner's key (with the owner's payload or
// not) gets the ordinary 404 — never the owner's payment, nor a 409 that
// would reveal the key exists — and the owner's data is untouched.
func TestApp_PaymentIdempotency_CrossTenantIsolation(t *testing.T) {
	handler, db := newTestApp(t)
	owner := registerTenant(t, handler, db, "Idempotency Owner Org", "idempotency-owner-"+uuid.NewString()+"@example.com")
	other := registerTenant(t, handler, db, "Idempotency Other Org", "idempotency-other-"+uuid.NewString()+"@example.com")
	invoiceID := createSentInvoice(t, handler, owner.token)
	path := "/api/v1/invoices/" + invoiceID + "/payments"
	body := `{"amount":300,"paymentMethod":"cash","paymentDate":"2026-01-15"}`
	key := uuid.NewString()

	if recorder := doRequestWithIdempotencyKey(handler, path, owner.token, body, key); recorder.Code != http.StatusCreated {
		t.Fatalf("owner: expected %d, got %d", http.StatusCreated, recorder.Code)
	}

	for _, attempt := range []string{body, `{"amount":1,"paymentMethod":"cash"}`} {
		recorder := doRequestWithIdempotencyKey(handler, path, other.token, attempt, key)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("expected %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
		}
	}

	// The other tenant may use the very same key on its own invoice.
	otherInvoiceID := createSentInvoice(t, handler, other.token)
	if recorder := doRequestWithIdempotencyKey(handler, "/api/v1/invoices/"+otherInvoiceID+"/payments", other.token, body, key); recorder.Code != http.StatusCreated || recorder.Header().Get("Idempotent-Replayed") != "" {
		t.Errorf("expected the other tenant's own payment under the same key to be newly created, got %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	if got := paymentCount(t, handler, owner.token, invoiceID); got != 1 {
		t.Errorf("expected the owner's invoice to still have exactly 1 payment, got %d", got)
	}
}
