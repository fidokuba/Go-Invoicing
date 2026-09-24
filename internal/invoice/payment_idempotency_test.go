package invoice

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newTestIdempotencyKey returns a fresh, valid Idempotency-Key for tests
// that need one but aren't about idempotency itself.
func newTestIdempotencyKey() string {
	return "test-" + uuid.NewString()
}

// --- Key validation ---

func TestValidateIdempotencyKey(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		valid bool
	}{
		{"minimum length (16)", strings.Repeat("a", 16), true},
		{"maximum length (128)", strings.Repeat("a", 128), true},
		{"too short (15)", strings.Repeat("a", 15), false},
		{"too long (129)", strings.Repeat("a", 129), false},
		{"empty", "", false},
		{"UUID", "8e03978e-40d5-43e8-bc93-6894a57f9324", true},
		{"upper-case UUID", "8E03978E-40D5-43E8-BC93-6894A57F9324", true},
		{"every permitted punctuation", "abc.def_ghi~jkl:mno-pqr", true},
		{"non-UUID opaque", "01J8ZQ4X7K3M9N2P5R6S8T0V1W", true},
		{"space", "abcdefgh ijklmnop", false},
		{"comma (list syntax)", "abcdefghij,klmnopq", false},
		{"double quote (sf-string)", `"abcdefghijklmnop"`, false},
		{"slash", "abcdefgh/ijklmnop", false},
		{"plus", "abcdefgh+ijklmnop", false},
		{"non-ASCII", "abcdefghijklmnopé", false},
		{"control character", "abcdefghijklmnop\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdempotencyKey(tt.key)
			if tt.valid && err != nil {
				t.Errorf("expected %q to be valid, got %v", tt.key, err)
			}
			if !tt.valid && err != ErrPaymentIdempotencyKeyInvalid {
				t.Errorf("expected %q to be rejected with ErrPaymentIdempotencyKeyInvalid, got %v", tt.key, err)
			}
		})
	}
}

// Keys are case-sensitive: two keys differing only in case are distinct
// identities (proven end-to-end in the service tests; here only that
// validation never folds case, since both forms are independently valid).
func TestValidateIdempotencyKey_CaseIsPreservedNotFolded(t *testing.T) {
	lower := "payment-attempt-abcdef"
	upper := strings.ToUpper(lower)

	if ValidateIdempotencyKey(lower) != nil || ValidateIdempotencyKey(upper) != nil {
		t.Fatal("expected both case variants to be valid keys")
	}
}

// --- Fingerprint ---

func fingerprintBaseRequest() CreatePaymentRequest {
	return CreatePaymentRequest{
		Amount:        50000,
		PaymentMethod: "bank_transfer",
		PaymentDate:   time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
		Reference:     "TX-90210",
		Notes:         "Second instalment",
	}
}

func TestFingerprintPaymentRequest_IsDeterministicSHA256(t *testing.T) {
	a := fingerprintPaymentRequest(fingerprintBaseRequest())
	b := fingerprintPaymentRequest(fingerprintBaseRequest())

	if len(a) != 32 {
		t.Fatalf("expected a 32-byte SHA-256 fingerprint, got %d bytes", len(a))
	}
	if !bytes.Equal(a, b) {
		t.Error("expected identical requests to fingerprint identically")
	}
}

// The idempotency key is deliberately not part of the fingerprint.
func TestFingerprintPaymentRequest_ExcludesIdempotencyKey(t *testing.T) {
	a := fingerprintBaseRequest()
	a.IdempotencyKey = "key-aaaaaaaaaaaaaaaa"
	b := fingerprintBaseRequest()
	b.IdempotencyKey = "key-bbbbbbbbbbbbbbbb"

	if !bytes.Equal(fingerprintPaymentRequest(a), fingerprintPaymentRequest(b)) {
		t.Error("expected the idempotency key to have no effect on the fingerprint")
	}
}

func TestFingerprintPaymentRequest_EveryLogicalFieldAffectsHash(t *testing.T) {
	base := fingerprintPaymentRequest(fingerprintBaseRequest())

	mutations := map[string]func(*CreatePaymentRequest){
		"amount":         func(r *CreatePaymentRequest) { r.Amount = 50001 },
		"payment method": func(r *CreatePaymentRequest) { r.PaymentMethod = "cash" },
		"payment date":   func(r *CreatePaymentRequest) { r.PaymentDate = r.PaymentDate.AddDate(0, 0, 1) },
		"date omitted":   func(r *CreatePaymentRequest) { r.PaymentDate = time.Time{} },
		"reference":      func(r *CreatePaymentRequest) { r.Reference = "TX-90211" },
		"reference gone": func(r *CreatePaymentRequest) { r.Reference = "" },
		"notes":          func(r *CreatePaymentRequest) { r.Notes = "Final instalment" },
		"notes gone":     func(r *CreatePaymentRequest) { r.Notes = "" },
		"method case":    func(r *CreatePaymentRequest) { r.PaymentMethod = "Bank_Transfer" },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			request := fingerprintBaseRequest()
			mutate(&request)

			if bytes.Equal(base, fingerprintPaymentRequest(request)) {
				t.Errorf("expected changing %s to change the fingerprint", name)
			}
		})
	}
}

// Values CreatePayment stores identically must fingerprint identically:
// reference/notes are whitespace-trimmed and blank means absent
// (nilIfEmpty), and only a payment date's calendar day matters.
func TestFingerprintPaymentRequest_EquivalentNormalizedValuesMatch(t *testing.T) {
	base := fingerprintPaymentRequest(fingerprintBaseRequest())

	padded := fingerprintBaseRequest()
	padded.Reference = "  TX-90210\t"
	padded.Notes = "\nSecond instalment "
	if !bytes.Equal(base, fingerprintPaymentRequest(padded)) {
		t.Error("expected surrounding whitespace on reference/notes to be ignored")
	}

	blank := fingerprintBaseRequest()
	blank.Reference, blank.Notes = "", ""
	whitespaceOnly := fingerprintBaseRequest()
	whitespaceOnly.Reference, whitespaceOnly.Notes = "   ", "\t"
	if !bytes.Equal(fingerprintPaymentRequest(blank), fingerprintPaymentRequest(whitespaceOnly)) {
		t.Error("expected blank and whitespace-only reference/notes to match (both stored as NULL)")
	}
}

// An omitted date fingerprints as "omitted", never as the time.Now()
// default CreatePayment later substitutes — so it is inherently stable
// across a midnight boundary (it has no dependency on the clock at all),
// and it differs from explicitly supplying today's date.
func TestFingerprintPaymentRequest_OmittedDateIsStableAndDistinct(t *testing.T) {
	omitted := fingerprintBaseRequest()
	omitted.PaymentDate = time.Time{}

	first := fingerprintPaymentRequest(omitted)
	second := fingerprintPaymentRequest(omitted)
	if !bytes.Equal(first, second) {
		t.Fatal("expected an omitted-date request to fingerprint identically every time")
	}

	for _, day := range []time.Time{
		time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		time.Now().UTC().Truncate(24 * time.Hour),
	} {
		explicit := fingerprintBaseRequest()
		explicit.PaymentDate = day
		if bytes.Equal(first, fingerprintPaymentRequest(explicit)) {
			t.Errorf("expected an omitted date to differ from an explicit %s", day.Format(dateLayout))
		}
	}
}

// newTestPaymentIdempotency returns a valid key/fingerprint pair for
// repository-level tests that insert payments directly.
func newTestPaymentIdempotency() PaymentIdempotency {
	return PaymentIdempotency{
		Key:         newTestIdempotencyKey(),
		RequestHash: fingerprintPaymentRequest(CreatePaymentRequest{Amount: 1, IdempotencyKey: uuid.NewString()}),
	}
}
