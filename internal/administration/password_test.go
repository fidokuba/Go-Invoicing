package admin

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// deriveKeyForTest and encodeHashForTest exist only so
// TestVerifyPassword_UsesParametersEncodedInHash can construct a hash
// under parameters different from today's argon2Memory/argon2Iterations/
// argon2Parallelism constants, without hashPassword itself needing a
// parameters argument it has no other reason to expose.
func deriveKeyForTest(password string, salt []byte, iterations, memory uint32, parallelism uint8, keyLength uint32) []byte {
	return argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
}

func encodeHashForTest(iterations, memory uint32, parallelism uint8, salt, key []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		memory,
		iterations,
		parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func TestHashPassword_Succeeds(t *testing.T) {
	encoded, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if encoded == "" {
		t.Fatal("expected a non-empty encoded hash")
	}
}

func TestHashPassword_DifferentSaltsProduceDifferentHashes(t *testing.T) {
	const password = "correct horse battery staple"

	first, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hash password (first): %v", err)
	}

	second, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hash password (second): %v", err)
	}

	if first == second {
		t.Fatal("expected two hashes of the same password to differ (random salts), but they were identical")
	}
}

func TestHashPassword_ContainsArgon2idPHCInformation(t *testing.T) {
	encoded, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	segments := strings.Split(encoded, "$")
	if len(segments) != 6 {
		t.Fatalf("expected 6 '$'-separated segments, got %d (%q)", len(segments), encoded)
	}

	if segments[1] != "argon2id" {
		t.Errorf("expected algorithm tag %q, got %q", "argon2id", segments[1])
	}

	if segments[2] != "v=19" {
		t.Errorf("expected version %q, got %q", "v=19", segments[2])
	}

	wantParams := "m=19456,t=2,p=1"
	if segments[3] != wantParams {
		t.Errorf("expected parameters %q, got %q", wantParams, segments[3])
	}
}

func TestVerifyPassword_CorrectPasswordSucceeds(t *testing.T) {
	const password = "correct horse battery staple"

	encoded, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if err := verifyPassword(password, encoded); err != nil {
		t.Errorf("expected the correct password to verify, got %v", err)
	}
}

func TestVerifyPassword_WrongPasswordFails(t *testing.T) {
	encoded, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	err = verifyPassword("wrong password entirely", encoded)
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("expected ErrPasswordMismatch, got %v", err)
	}
}

func TestVerifyPassword_MalformedHash_WrongSegmentCount(t *testing.T) {
	err := verifyPassword("anything", "not-a-valid-hash")
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("expected ErrMalformedHash, got %v", err)
	}
}

func TestVerifyPassword_MalformedHash_BadBase64(t *testing.T) {
	// Well-shaped (6 segments, argon2id, valid version/params) but the
	// salt segment is not valid base64.
	malformed := "$argon2id$v=19$m=19456,t=2,p=1$not-valid-base64!!!$c29tZWtleQ"

	err := verifyPassword("anything", malformed)
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("expected ErrMalformedHash, got %v", err)
	}
}

func TestVerifyPassword_MalformedHash_BadParameters(t *testing.T) {
	// Parameters segment doesn't match the expected "m=..,t=..,p=.." shape.
	malformed := "$argon2id$v=19$not-params-at-all$c29tZXNhbHQ$c29tZWtleQ"

	err := verifyPassword("anything", malformed)
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("expected ErrMalformedHash, got %v", err)
	}
}

func TestVerifyPassword_UnsupportedVariant(t *testing.T) {
	// Well-shaped, but a different Argon2 variant (argon2i, not argon2id).
	unsupported := "$argon2i$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ"

	err := verifyPassword("anything", unsupported)
	if !errors.Is(err, ErrUnsupportedHashVariant) {
		t.Errorf("expected ErrUnsupportedHashVariant, got %v", err)
	}
}

func TestVerifyPassword_DoesNotPanicOnGarbageInput(t *testing.T) {
	// A grab-bag of adversarial inputs that must all fail cleanly, never
	// panic — including empty string and strings with unexpected '$'
	// placement.
	inputs := []string{
		"",
		"$",
		"$$$$$",
		"$argon2id$$$$",
		"argon2id-no-leading-dollar",
	}

	for _, input := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("verifyPassword panicked on input %q: %v", input, r)
				}
			}()

			if err := verifyPassword("anything", input); err == nil {
				t.Errorf("expected an error for malformed input %q, got nil", input)
			}
		}()
	}
}

func TestVerifyPassword_UsesParametersEncodedInHash(t *testing.T) {
	// Hash with parameters deliberately different from today's constants,
	// to prove verifyPassword parses and uses the encoded parameters
	// rather than assuming argon2Memory/argon2Iterations/argon2Parallelism.
	// This is what lets already-stored hashes keep working if those
	// constants are retuned later.
	const password = "correct horse battery staple"

	salt := []byte("0123456789abcdef") // 16 bytes, matches argon2SaltLength
	key := deriveKeyForTest(password, salt, 1, 8192, 2, 32)

	encoded := encodeHashForTest(1, 8192, 2, salt, key)

	if err := verifyPassword(password, encoded); err != nil {
		t.Errorf("expected verification against non-default encoded parameters to succeed, got %v", err)
	}

	if err := verifyPassword("wrong password", encoded); !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("expected ErrPasswordMismatch for the wrong password, got %v", err)
	}
}
