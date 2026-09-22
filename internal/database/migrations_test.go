package database

import (
	"errors"
	"strings"
	"testing"
)

// secretMarker stands in for a real database password in these tests —
// distinctive enough that its presence or absence in an error string is
// unambiguous.
const secretMarker = "SUPERSECRETPW123"

// TestSanitizeDSNError_RedactsDatabaseURLWhenPresent proves the one
// concrete leak path sanitizeDSNError exists for: some errors (see its
// own doc comment — a malformed postgres:// URL surfaced through
// net/url.Parse is the reproduced case) embed the exact DSN string
// verbatim, credentials included.
func TestSanitizeDSNError_RedactsDatabaseURLWhenPresent(t *testing.T) {
	databaseURL := "postgres://myuser:" + secretMarker + "@127.0.0.1:notaport/mydb"
	err := errors.New(`parse "` + databaseURL + `": invalid port ":notaport" after host`)

	sanitized := sanitizeDSNError(err, databaseURL)

	if sanitized == nil {
		t.Fatal("expected a non-nil sanitized error")
	}
	if strings.Contains(sanitized.Error(), secretMarker) {
		t.Fatalf("expected the credential to be redacted, got: %s", sanitized.Error())
	}
	if strings.Contains(sanitized.Error(), databaseURL) {
		t.Fatalf("expected the full database URL to be redacted, got: %s", sanitized.Error())
	}
	if !strings.Contains(sanitized.Error(), "[redacted database URL]") {
		t.Fatalf("expected the redaction placeholder, got: %s", sanitized.Error())
	}
}

// TestSanitizeDSNError_LeavesUnrelatedErrorsUntouched proves the common
// case — an error that never echoed the DSN at all, e.g. an ordinary
// "connection refused" (pgx and lib/pq both already omit the DSN/password
// from this kind of error; see this package's own audit notes) — passes
// through completely unchanged.
func TestSanitizeDSNError_LeavesUnrelatedErrorsUntouched(t *testing.T) {
	databaseURL := "postgres://myuser:" + secretMarker + "@127.0.0.1:5432/mydb"
	err := errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")

	sanitized := sanitizeDSNError(err, databaseURL)

	if sanitized != err {
		t.Fatalf("expected an unrelated error to pass through unchanged, got: %v", sanitized)
	}
}

// TestSanitizeDSNError_NilErrorAndEmptyURL proves both edge inputs are
// handled without panicking.
func TestSanitizeDSNError_NilErrorAndEmptyURL(t *testing.T) {
	if got := sanitizeDSNError(nil, "postgres://x"); got != nil {
		t.Fatalf("expected nil in, nil out, got: %v", got)
	}

	err := errors.New("some error")
	if got := sanitizeDSNError(err, ""); got != err {
		t.Fatalf("expected an empty databaseURL to leave the error untouched, got: %v", got)
	}
}

// TestMigrate_MalformedDatabaseURL_NeverLeaksCredentialInReturnedError is
// an end-to-end reproduction through the real Migrate function (no live
// Postgres required — a malformed postgres:// URL fails during DSN
// parsing, before any network connection is attempted): the returned
// error must never contain the raw credential.
func TestMigrate_MalformedDatabaseURL_NeverLeaksCredentialInReturnedError(t *testing.T) {
	databaseURL := "postgres://myuser:" + secretMarker + "@127.0.0.1:notaport/mydb"

	err := Migrate(databaseURL)
	if err == nil {
		t.Fatal("expected Migrate to fail for a malformed database URL")
	}
	if strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("expected no credential leak in Migrate's returned error, got: %s", err.Error())
	}
}
