package admin

import "testing"

// FuzzDecodeHash targets decodeHash specifically: it's the one hand-rolled
// parser in this codebase with real structural complexity (segment
// splitting, base64 decoding, Sscanf-based numeric parsing) sitting
// behind a boundary that could plausibly see attacker-influenced input
// one day (e.g. a future hash-migration path reading stored values from
// elsewhere). The existing table-driven tests in password_test.go
// (TestVerifyPassword_MalformedHash_*, TestVerifyPassword_
// DoesNotPanicOnGarbageInput) already cover the known-important malformed
// shapes; fuzzing exists to find the shapes nobody thought to write by
// hand.
//
// The invariant under fuzzing is deliberately narrow: decodeHash must
// never panic on any input, no matter how malformed. Correctness (that
// well-formed hashes decode correctly and specific malformed ones return
// the right sentinel error) is already covered elsewhere.
func FuzzDecodeHash(f *testing.F) {
	seeds := []string{
		"",
		"$",
		"$$$$$",
		"$argon2id$$$$",
		"argon2id-no-leading-dollar",
		"$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		"$argon2i$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		"$argon2id$v=19$not-params-at-all$c29tZXNhbHQ$c29tZWtleQ",
		"$argon2id$v=19$m=19456,t=2,p=1$not-valid-base64!!!$c29tZWtleQ",
		"$argon2id$v=abc$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		"$argon2id$v=19$m=99999999999999999999,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		"$argon2id$v=19$m=-1,t=-1,p=-1$c29tZXNhbHQ$c29tZWtleQ",
		"$argon2id$v=19$m=19456,t=2,p=1$$",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, encodedHash string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("decodeHash panicked on input %q: %v", encodedHash, r)
			}
		}()

		_, _, _, _, _, _, _ = decodeHash(encodedHash)
	})
}
