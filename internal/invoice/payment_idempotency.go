package invoice

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
)

// Milestone 13 Part 1: idempotent payment creation.
//
// POST /invoices/{id}/payments requires an Idempotency-Key. The key and a
// fingerprint of the logical request are stored directly on the payment
// row that request created (payments.idempotency_key/request_hash), in the
// same transaction — see InvoiceService.CreatePayment for the ordering
// and 000015_add_payments_idempotency.up.sql for the schema. There is
// deliberately no generic idempotency abstraction here: payment creation
// is the only operation that needs one, so everything lives beside it.

var (
	// ErrPaymentIdempotencyKeyInvalid is returned when an Idempotency-Key
	// is missing, empty, or isn't 16-128 characters from the permitted set
	// (see ValidateIdempotencyKey).
	ErrPaymentIdempotencyKeyInvalid = errors.New("idempotency key must be 16-128 characters from A-Z a-z 0-9 . _ ~ : -")

	// ErrPaymentIdempotencyKeyReused is returned when a key has already
	// been used, on the same invoice, by a successfully recorded payment
	// whose request fingerprint differs from this one. Its message is
	// deliberately generic: it must never reveal anything about the
	// original request (amount, payment ID, hash, or the key itself).
	ErrPaymentIdempotencyKeyReused = errors.New("idempotency key has already been used for a different payment request")
)

const (
	idempotencyKeyMinLength = 16
	idempotencyKeyMaxLength = 128

	// paymentFingerprintVersion prefixes every canonical encoding, so the
	// fingerprint's field set can change in future without an old stored
	// hash ever accidentally matching a differently-shaped new one.
	paymentFingerprintVersion = "payment.create.v1"
)

// ValidateIdempotencyKey reports whether key is an acceptable
// Idempotency-Key: 16-128 characters, each one of A-Z a-z 0-9 . _ ~ : -
// (the pattern ^[A-Za-z0-9._~:-]{16,128}$). Keys are opaque — a UUID is
// accepted but not required — and case-sensitive: they are compared
// byte-for-byte, never normalised.
func ValidateIdempotencyKey(key string) error {
	if len(key) < idempotencyKeyMinLength || len(key) > idempotencyKeyMaxLength {
		return ErrPaymentIdempotencyKeyInvalid
	}

	for i := 0; i < len(key); i++ {
		if !isIdempotencyKeyByte(key[i]) {
			return ErrPaymentIdempotencyKeyInvalid
		}
	}

	return nil
}

func isIdempotencyKeyByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '.', c == '_', c == '~', c == ':', c == '-':
		return true
	default:
		return false
	}
}

// paymentFingerprintV1 is the canonical logical shape of a payment
// request. Each field is normalised exactly the way CreatePayment stores
// it, so two requests that would record the same payment always
// fingerprint identically, regardless of JSON whitespace/key order or
// "" vs an omitted optional field:
//
//   - PaymentMethod: as supplied (CreatePayment stores it untrimmed).
//   - PaymentDate: "" when omitted, otherwise "YYYY-MM-DD". An omitted
//     date is fingerprinted as omitted — never as the time.Now() default
//     CreatePayment later substitutes — so a retry after midnight still
//     matches the original request. An explicit date equal to today is a
//     different logical request and fingerprints differently.
//   - Reference, Notes: whitespace-trimmed, "" when blank (nilIfEmpty's
//     rule — blank is stored as NULL).
//
// Tenant, invoice and the key itself are deliberately absent: the key is
// already scoped to a tenant-verified invoice, so including them would
// add nothing.
type paymentFingerprintV1 struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"paymentMethod"`
	PaymentDate   string `json:"paymentDate"`
	Reference     string `json:"reference"`
	Notes         string `json:"notes"`
}

// fingerprintPaymentRequest returns the SHA-256 of request's versioned
// canonical encoding. It must be called on the request as received —
// before any default is applied to a zero PaymentDate.
//
// encoding/json marshals struct fields in declaration order, so the
// encoding (and therefore the hash) is deterministic.
func fingerprintPaymentRequest(request CreatePaymentRequest) []byte {
	var paymentDate string
	if !request.PaymentDate.IsZero() {
		paymentDate = request.PaymentDate.Format(dateLayout)
	}

	canonical, err := json.Marshal(paymentFingerprintV1{
		Amount:        request.Amount,
		PaymentMethod: request.PaymentMethod,
		PaymentDate:   paymentDate,
		Reference:     strings.TrimSpace(request.Reference),
		Notes:         strings.TrimSpace(request.Notes),
	})
	if err != nil {
		// Unreachable: the struct contains only strings and an int64.
		panic("invoice: marshal payment fingerprint: " + err.Error())
	}

	sum := sha256.Sum256(append([]byte(paymentFingerprintVersion+"\x00"), canonical...))

	return sum[:]
}
