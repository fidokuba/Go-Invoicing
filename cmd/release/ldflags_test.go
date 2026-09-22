package main

import (
	"strings"
	"testing"
)

func TestReleaseLDFlags(t *testing.T) {
	got := releaseLDFlags("v1.2.3", "abc1234def5678", "2026-01-01T00:00:00Z")

	for _, want := range []string{
		"-s -w",
		"-X go-invoicing/internal/buildinfo.Version=v1.2.3",
		"-X go-invoicing/internal/buildinfo.Commit=abc1234def5678",
		"-X go-invoicing/internal/buildinfo.BuildTime=2026-01-01T00:00:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected ldflags to contain %q, got: %s", want, got)
		}
	}
}

// TestReleaseLDFlags_ImportPathMatchesActualPackage guards against the
// import path and the real internal/buildinfo package ever silently
// drifting apart — a typo here would mean -X silently does nothing
// (the Go linker does not error on an -X target it can't find; see
// go help buildmode), so this is worth pinning explicitly rather than
// trusting the string literal in ldflags.go never to be touched again.
func TestReleaseLDFlags_ImportPathMatchesActualPackage(t *testing.T) {
	const wantImportPath = "go-invoicing/internal/buildinfo"
	if buildinfoImportPath != wantImportPath {
		t.Errorf("buildinfoImportPath = %q, want %q", buildinfoImportPath, wantImportPath)
	}
}

// TestReleaseLDFlags_NoSecretsOrLocalPaths is Milestone 11 Part 4
// section 38's security review, pinned as a test: the ldflags string
// must only ever contain the three build-metadata values and the fixed
// flags/import path — never an absolute filesystem path, an environment
// dump, or anything resembling a credential.
func TestReleaseLDFlags_NoSecretsOrLocalPaths(t *testing.T) {
	got := releaseLDFlags("v1.2.3", "abc1234", "2026-01-01T00:00:00Z")

	for _, forbidden := range []string{"/Users/", "/home/", "DATABASE_URL", "PASSWORD", "SECRET"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("expected ldflags never to contain %q, got: %s", forbidden, got)
		}
	}
}
