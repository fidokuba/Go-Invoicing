package invoice

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"go-invoicing/internal/httpx"
	"go-invoicing/internal/metrics"
)

// Milestone 13 Part 1: the HTTP contract of idempotent payment creation.

const idempotencyTestBody = `{"amount": 4000, "paymentMethod": "bank_transfer", "paymentDate": "2026-09-19", "reference": "TX-90210"}`

// postPayment sends POST /invoices/{id}/payments as organisationID with
// the given Idempotency-Key header values (none if keys is empty).
func postPayment(handler *InvoiceHandler, invoiceID, organisationID uuid.UUID, body string, keys ...string) *httptest.ResponseRecorder {
	request := newPaymentTestRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", bytes.NewBufferString(body), invoiceID, organisationID)
	request.Header.Del("Idempotency-Key")
	for _, key := range keys {
		request.Header.Add("Idempotency-Key", key)
	}

	recorder := httptest.NewRecorder()
	handler.CreatePayment(recorder, request)

	return recorder
}

func decodeErrorBody(t *testing.T, recorder *httptest.ResponseRecorder) httpx.ErrorDetail {
	t.Helper()

	var body httpx.ErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (body: %s)", err, recorder.Body.String())
	}

	return body.Error
}

func decodePaymentBody(t *testing.T, recorder *httptest.ResponseRecorder) PaymentResponse {
	t.Helper()

	var response PaymentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode payment: %v (body: %s)", err, recorder.Body.String())
	}

	return response
}

func TestInvoiceHandler_CreatePayment_IdempotencyKeyValidation(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"missing", nil},
		{"empty", []string{""}},
		{"too short", []string{strings.Repeat("k", 15)}},
		{"too long", []string{strings.Repeat("k", 129)}},
		{"invalid characters", []string{"not a valid key!!"}},
		{"quoted sf-string form", []string{`"8e03978e-40d5-43e8-bc93-6894a57f9324"`}},
		{"two headers", []string{newTestIdempotencyKey(), newTestIdempotencyKey()}},
		{"two identical headers", []string{"same-key-0123456789", "same-key-0123456789"}},
		{"comma-joined list", []string{newTestIdempotencyKey() + ", " + newTestIdempotencyKey()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTestFixture()
			handler := newTestHandler(f)
			invoiceID := f.addInvoice(10000, InvoiceStatusSent)

			recorder := postPayment(handler, invoiceID, f.organisationID, idempotencyTestBody, tt.keys...)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
			}
			if got := decodeErrorBody(t, recorder); got.Code != httpx.CodeInvalidRequest || !strings.Contains(got.Message, "Idempotency-Key") {
				t.Errorf("expected an invalid_request error about Idempotency-Key, got %+v", got)
			}
			if len(f.paymentRepository.payments[invoiceID]) != 0 {
				t.Error("expected no payment to be created")
			}
		})
	}
}

func TestInvoiceHandler_CreatePayment_IdempotencyKeyBoundariesAccepted(t *testing.T) {
	for _, key := range []string{strings.Repeat("k", 16), strings.Repeat("K", 128), uuid.NewString(), "a.b_c~d:e-f012345"} {
		f := newTestFixture()
		handler := newTestHandler(f)
		invoiceID := f.addInvoice(10000, InvoiceStatusSent)

		if recorder := postPayment(handler, invoiceID, f.organisationID, idempotencyTestBody, key); recorder.Code != http.StatusCreated {
			t.Errorf("key %q: expected status %d, got %d (body: %s)", key, http.StatusCreated, recorder.Code, recorder.Body.String())
		}
	}
}

// Authentication is resolved before anything idempotency-related: an
// unauthenticated request is a 401 regardless of its key.
func TestInvoiceHandler_CreatePayment_AuthenticationPrecedesIdempotencyKey(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	for name, keys := range map[string][]string{
		"no key":        nil,
		"malformed key": {"bad"},
		"valid key":     {newTestIdempotencyKey()},
	} {
		request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/payments", bytes.NewBufferString(idempotencyTestBody))
		request.Header.Set("Content-Type", "application/json")
		request.SetPathValue("id", invoiceID.String())
		for _, key := range keys {
			request.Header.Add("Idempotency-Key", key)
		}
		recorder := httptest.NewRecorder()

		handler.CreatePayment(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected status %d, got %d", name, http.StatusUnauthorized, recorder.Code)
		}
	}
}

func TestInvoiceHandler_CreatePayment_ReplayReturnsOriginal201WithHeader(t *testing.T) {
	f := newTestFixture()
	m := metrics.New(nil)
	handler := NewInvoiceHandler(f.service, f.pdfService(), m)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	key := newTestIdempotencyKey()

	first := postPayment(handler, invoiceID, f.organisationID, idempotencyTestBody, key)
	if first.Code != http.StatusCreated {
		t.Fatalf("first attempt: expected %d, got %d (body: %s)", http.StatusCreated, first.Code, first.Body.String())
	}
	if got := first.Header().Get("Idempotent-Replayed"); got != "" {
		t.Errorf("expected no Idempotent-Replayed header on the original response, got %q", got)
	}

	// Same logical request, different JSON whitespace and key order.
	retryBody := `{"reference":"TX-90210","paymentDate":"2026-09-19","paymentMethod":"bank_transfer","amount":4000}`
	replay := postPayment(handler, invoiceID, f.organisationID, retryBody, key)
	if replay.Code != http.StatusCreated {
		t.Fatalf("retry: expected %d, got %d (body: %s)", http.StatusCreated, replay.Code, replay.Body.String())
	}
	if got := replay.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Errorf("expected Idempotent-Replayed: true on the replay, got %q", got)
	}

	if first.Body.String() != replay.Body.String() {
		t.Errorf("expected the replay body to equal the original byte-for-byte:\noriginal: %s\nreplayed: %s", first.Body.String(), replay.Body.String())
	}
	if got := len(f.paymentRepository.payments[invoiceID]); got != 1 {
		t.Errorf("expected exactly 1 payment, got %d", got)
	}

	body := scrapeMetrics(t, m)
	for _, want := range []string{
		`go_invoicing_payment_idempotency_total{outcome="created"} 1`,
		`go_invoicing_payment_idempotency_total{outcome="replayed"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in metrics", want)
		}
	}
}

func TestInvoiceHandler_CreatePayment_FullPaymentRetryReplays(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	key := newTestIdempotencyKey()
	body := `{"amount": 10000, "paymentMethod": "cash", "paymentDate": "2026-09-19"}`

	first := postPayment(handler, invoiceID, f.organisationID, body, key)
	if first.Code != http.StatusCreated {
		t.Fatalf("first attempt: expected %d, got %d (body: %s)", http.StatusCreated, first.Code, first.Body.String())
	}

	replay := postPayment(handler, invoiceID, f.organisationID, body, key)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("expected a 201 replay (not a 409 for the now-Paid invoice), got %d (body: %s)", replay.Code, replay.Body.String())
	}
	if decodePaymentBody(t, first).ID != decodePaymentBody(t, replay).ID {
		t.Error("expected the replay to return the original payment")
	}

	// A new key is a new payment, which the Paid invoice refuses.
	if recorder := postPayment(handler, invoiceID, f.organisationID, body, newTestIdempotencyKey()); recorder.Code != http.StatusConflict || decodeErrorBody(t, recorder).Code != httpx.CodeConflict {
		t.Errorf("expected a new payment against the Paid invoice to be a plain 409 conflict, got %d (body: %s)", recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_CreatePayment_KeyReusedWithDifferentPayloadIs409(t *testing.T) {
	f := newTestFixture()
	m := metrics.New(nil)
	handler := NewInvoiceHandler(f.service, f.pdfService(), m)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	key := newTestIdempotencyKey()

	first := postPayment(handler, invoiceID, f.organisationID, idempotencyTestBody, key)
	if first.Code != http.StatusCreated {
		t.Fatalf("first attempt: expected %d, got %d", http.StatusCreated, first.Code)
	}
	originalID := decodePaymentBody(t, first).ID

	recorder := postPayment(handler, invoiceID, f.organisationID, `{"amount": 7777, "paymentMethod": "bank_transfer", "paymentDate": "2026-09-19"}`, key)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
	got := decodeErrorBody(t, recorder)
	if got.Code != "idempotency_key_reused" {
		t.Errorf("expected code idempotency_key_reused, got %q", got.Code)
	}
	if got.Message != "Idempotency-Key has already been used for a different payment request." {
		t.Errorf("unexpected message %q", got.Message)
	}
	if recorder.Header().Get("Idempotent-Replayed") != "" {
		t.Error("expected no Idempotent-Replayed header on a conflict")
	}

	// Nothing about the original request may leak through the conflict.
	raw := recorder.Body.String()
	for _, secret := range []string{key, originalID, "4000", "TX-90210"} {
		if strings.Contains(raw, secret) {
			t.Errorf("expected the conflict body not to reveal %q, got %s", secret, raw)
		}
	}

	if n := len(f.paymentRepository.payments[invoiceID]); n != 1 {
		t.Errorf("expected exactly 1 payment, got %d", n)
	}
	if !strings.Contains(scrapeMetrics(t, m), `go_invoicing_payment_idempotency_total{outcome="conflict"} 1`) {
		t.Error("expected one conflict outcome to be recorded")
	}
}

// A key already used by the invoice's owner, sent by another tenant, is
// an ordinary 404 — never a replay (the owner's payment) nor a 409
// (revealing the key exists).
func TestInvoiceHandler_CreatePayment_CrossTenantKeyIsNotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	key := newTestIdempotencyKey()

	if recorder := postPayment(handler, invoiceID, f.organisationID, idempotencyTestBody, key); recorder.Code != http.StatusCreated {
		t.Fatalf("owner: expected %d, got %d", http.StatusCreated, recorder.Code)
	}

	for _, body := range []string{idempotencyTestBody, `{"amount": 1, "paymentMethod": "cash"}`} {
		recorder := postPayment(handler, invoiceID, uuid.New(), body, key)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
		}
	}
}

// Ordinary failures don't consume the key: after a rejected overpayment,
// the same key with a corrected amount is recorded normally.
func TestInvoiceHandler_CreatePayment_FailureDoesNotConsumeKey(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)
	key := newTestIdempotencyKey()

	if recorder := postPayment(handler, invoiceID, f.organisationID, `{"amount": 20000, "paymentMethod": "cash"}`, key); recorder.Code != http.StatusConflict || decodeErrorBody(t, recorder).Code != httpx.CodeConflict {
		t.Fatalf("expected an overpayment 409 conflict, got %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	recorder := postPayment(handler, invoiceID, f.organisationID, `{"amount": 5000, "paymentMethod": "cash"}`, key)
	if recorder.Code != http.StatusCreated || recorder.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("expected a newly created 201, got %d (body: %s)", recorder.Code, recorder.Body.String())
	}
}
