package main

import (
	"io"
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
