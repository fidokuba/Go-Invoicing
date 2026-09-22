package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// buildTarget cross-compiles ./cmd/api for t into distDir/name, using
// ldflags (see releaseLDFlags). CGO_ENABLED=0 is set explicitly and
// unconditionally here — never inherited from the invoking shell/host —
// so the release build behaves identically regardless of the developer
// or CI runner's own local environment (Milestone 11 Part 1 already
// proved this application needs no CGO for any of the five targets).
//
// -trimpath is passed alongside ldflags for the same reason -s -w is:
// keeping build-machine-specific detail out of the shipped binary — in
// this case the local filesystem path this repository happens to be
// checked out under, which would otherwise be embedded in panic stack
// traces and the module build-info blob (`go version -m`) for no
// benefit to anyone running the binary.
func buildTarget(t target, distDir, ldflags, version string) (outPath string, err error) {
	name := t.artifactName(version)
	outPath = filepath.Join(distDir, name)

	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", outPath, "./cmd/api")
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+t.goos,
		"GOARCH="+t.goarch,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build %s/%s: %w", t.goos, t.goarch, err)
	}

	// go build already creates its output file executable on Unix; this
	// is a small, explicit belt-and-braces step so the artifact's
	// permissions don't depend on the build host's umask. Irrelevant
	// (and skipped) for the Windows .exe, where Unix executable bits
	// have no meaning.
	if t.goos != "windows" {
		if err := os.Chmod(outPath, 0o755); err != nil {
			return "", fmt.Errorf("chmod %s: %w", outPath, err)
		}
	}

	return outPath, nil
}
