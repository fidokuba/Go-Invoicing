package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSHA256File_MatchesKnownDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}

	// sha256sum <<< "hello world" (echo adds the trailing newline the
	// file above also has) — a widely-known, independently reproducible
	// digest, not just "whatever this function happens to compute".
	const want = "a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447"
	if got != want {
		t.Errorf("sha256File() = %s, want %s", got, want)
	}
}

func TestSHA256File_MissingFile(t *testing.T) {
	if _, err := sha256File(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// TestFormatChecksums_StandardTwoSpaceFormat proves the output matches
// exactly what GNU coreutils' own `sha256sum -c` expects to parse:
// "<64-hex-digest><two spaces><filename>\n".
func TestFormatChecksums_StandardTwoSpaceFormat(t *testing.T) {
	entries := []checksumEntry{
		{hash: strings.Repeat("a", 64), filename: "go-invoicing_v1.0.0_linux_amd64"},
		{hash: strings.Repeat("b", 64), filename: "go-invoicing_v1.0.0_windows_amd64.exe"},
	}

	got := formatChecksums(entries)
	want := strings.Repeat("a", 64) + "  go-invoicing_v1.0.0_linux_amd64\n" +
		strings.Repeat("b", 64) + "  go-invoicing_v1.0.0_windows_amd64.exe\n"

	if got != want {
		t.Errorf("formatChecksums() =\n%q\nwant\n%q", got, want)
	}
}

// TestFormatChecksums_PreservesGivenOrder proves the formatter itself
// never reorders entries (e.g. alphabetically) — determinism is the
// caller's responsibility (iterating the fixed releaseTargets() list),
// and this formatter must not silently undermine that by sorting.
func TestFormatChecksums_PreservesGivenOrder(t *testing.T) {
	entries := []checksumEntry{
		{hash: "2", filename: "zzz"},
		{hash: "1", filename: "aaa"},
	}

	got := formatChecksums(entries)
	if !strings.HasPrefix(got, "2  zzz\n") {
		t.Errorf("expected the \"zzz\" entry first (input order), got:\n%s", got)
	}
}

func TestWriteChecksums_WritesReadableRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")

	entries := []checksumEntry{{hash: strings.Repeat("c", 64), filename: "example"}}
	if err := writeChecksums(path, entries); err != nil {
		t.Fatalf("writeChecksums: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat checksums file: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected a regular file, got a directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checksums file: %v", err)
	}
	if !strings.Contains(string(data), "example") {
		t.Errorf("expected the written file to contain the entry, got: %s", data)
	}
}
