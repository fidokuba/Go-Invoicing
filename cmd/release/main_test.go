package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestRun_MissingVersionFails and TestRun_InvalidVersionFails exercise
// only run's up-front validation — both return before ever touching
// git, dist/, or `go build`, so these stay fast, ordinary `go test ./...`
// tests. run's actual build-five-targets success path is deliberately
// NOT exercised here (see this milestone's own report): that is a real
// cross-compile of five binaries, proportionate to a dedicated, explicit
// smoke test rather than something every `go test ./...` run should pay
// for repeatedly.

func TestRun_MissingVersionFails(t *testing.T) {
	err := run(nil, io.Discard)
	if err == nil {
		t.Fatal("expected an error when -version is not supplied")
	}
	if !strings.Contains(err.Error(), "-version is required") {
		t.Errorf("expected the error to explain -version is required, got: %v", err)
	}
}

func TestRun_InvalidVersionFails(t *testing.T) {
	err := run([]string{"-version", "not-a-version"}, io.Discard)
	if err == nil {
		t.Fatal("expected an error for an invalid -version")
	}
	if !strings.Contains(err.Error(), "not-a-version") {
		t.Errorf("expected the error to reference the rejected value, got: %v", err)
	}
}

// TestRun_UnknownFlagFails proves an unrecognized flag is rejected
// outright (flag.ContinueOnError still returns an error the caller must
// handle) rather than silently ignored.
func TestRun_UnknownFlagFails(t *testing.T) {
	err := run([]string{"-not-a-real-flag", "x"}, io.Discard)
	if err == nil {
		t.Fatal("expected an error for an unrecognized flag")
	}
}

// --- -validate-only (Milestone 11 Part 6) ---

// TestRun_ValidateOnly_ValidVersionSucceedsWithoutBuilding proves
// -validate-only exits successfully for a valid version and never
// touches dist/ — the release workflow relies on exactly this to check a
// Git tag cheaply before any other release work happens.
func TestRun_ValidateOnly_ValidVersionSucceedsWithoutBuilding(t *testing.T) {
	dir := t.TempDir() + "/dist-should-not-be-created"

	var out bytes.Buffer
	err := run([]string{"-version", "v1.2.3", "-validate-only", "-dist", dir}, &out)
	if err != nil {
		t.Fatalf("expected -validate-only to succeed for a valid version, got: %v", err)
	}

	if !strings.Contains(out.String(), "v1.2.3") {
		t.Errorf("expected the output to confirm the validated version, got: %s", out.String())
	}

	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("expected -validate-only never to create %s, but it exists (stat err: %v)", dir, statErr)
	}
}

// TestRun_ValidateOnly_InvalidVersionFails proves -validate-only still
// rejects an invalid version exactly like the full build path does — the
// same validateReleaseVersion call, not a separate, looser check.
func TestRun_ValidateOnly_InvalidVersionFails(t *testing.T) {
	err := run([]string{"-version", "not-a-version", "-validate-only"}, io.Discard)
	if err == nil {
		t.Fatal("expected -validate-only to reject an invalid version")
	}
}

// TestRun_ValidateOnly_MissingVersionFails proves -validate-only doesn't
// bypass the "-version is required" check.
func TestRun_ValidateOnly_MissingVersionFails(t *testing.T) {
	err := run([]string{"-validate-only"}, io.Discard)
	if err == nil {
		t.Fatal("expected -validate-only with no -version to still fail")
	}
	if !strings.Contains(err.Error(), "-version is required") {
		t.Errorf("expected the error to explain -version is required, got: %v", err)
	}
}
