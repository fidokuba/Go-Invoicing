package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirExists(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "assets")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	realFile := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(realFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if !dirExists(realDir) {
		t.Error("expected an existing directory to report true")
	}
	if dirExists(filepath.Join(tmp, "does-not-exist")) {
		t.Error("expected a nonexistent path to report false")
	}
	if dirExists(realFile) {
		t.Error("expected an existing plain file to report false (it is not a directory)")
	}
}
