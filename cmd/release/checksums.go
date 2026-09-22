package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// checksumEntry is one line of the release's checksums.txt.
type checksumEntry struct {
	hash     string
	filename string
}

// sha256File hashes path's contents using Go's standard library
// crypto/sha256 — not a shelled-out `sha256sum`, so this works
// identically regardless of what's installed on the host (portable
// across Linux/macOS/Windows) and needs no external tool at all.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// formatChecksums renders entries as the standard two-space-separated
// "<hex-digest>  <filename>" lines GNU coreutils' own sha256sum both
// produces and verifies (`sha256sum -c checksums.txt`). entries is
// rendered in the order given — callers are responsible for that order
// being deterministic (see releaseTargets' own doc comment: the release
// build always iterates the same fixed target list, so entries always
// arrives in the same order across runs).
func formatChecksums(entries []checksumEntry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s  %s\n", e.hash, e.filename)
	}

	return b.String()
}

// writeChecksums writes formatChecksums' output to path as a plain
// regular file (0644 — readable by anyone, writable only by its owner;
// there is nothing sensitive in a checksum manifest).
func writeChecksums(path string, entries []checksumEntry) error {
	return os.WriteFile(path, []byte(formatChecksums(entries)), 0o644)
}
