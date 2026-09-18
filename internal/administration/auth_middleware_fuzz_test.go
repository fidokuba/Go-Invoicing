package admin

import "testing"

// FuzzParseBearerToken targets parseBearerToken specifically because it's
// a small, pure, string-in/string-out parser with real branching logic
// (whitespace splitting, case-insensitive scheme comparison) — exactly
// the shape of code fuzzing is good at finding panics in, that a
// hand-written table of known-important cases (see
// auth_middleware_test.go) can't exhaustively rule out.
//
// The invariant under fuzzing is deliberately narrow: parseBearerToken
// must never panic on any input. Correctness (which inputs are accepted
// vs rejected) is already covered by the table-driven tests; fuzzing
// isn't trying to duplicate that.
func FuzzParseBearerToken(f *testing.F) {
	seeds := []string{
		"",
		"Bearer",
		"Bearer ",
		"Bearer token",
		"bearer token",
		"BEARER token",
		"BeArEr token",
		"Basic dXNlcjpwYXNz",
		"Bearer token extra",
		"Bearer\ttoken",
		"   Bearer token   ",
		"Bearer\x00token",
		"Bearer " + string(rune(0x2028)), // unicode line separator, a classic whitespace-handling edge case
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, header string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("parseBearerToken panicked on input %q: %v", header, r)
			}
		}()

		_, _ = parseBearerToken(header)
	})
}
