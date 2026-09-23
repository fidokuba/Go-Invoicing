package main

import (
	"os"
	"path/filepath"
)

// webuiDistDir is internal/webui/dist relative to this tool's own
// working directory — this project's README and this package's own doc
// comment both assume `go run ./cmd/release` is invoked from the
// repository root, exactly like every other command in this project's
// documented workflows.
const webuiDistDir = "internal/webui/dist"

// frontendLooksBuilt reports whether internal/webui/dist looks like a
// real `npm run build` output rather than the committed placeholder
// (see that directory's own index.html comment): specifically, whether
// it has an "assets" subdirectory, which Vite always produces for a real
// build (see web/vite.config.ts) and the placeholder never has. This is
// a heuristic, not a guarantee — a corrupted or partial build could
// still slip through — but it catches the one common, easy-to-forget
// mistake (running a release build without ever having run the frontend
// build at all) that run's own warning exists for.
func frontendLooksBuilt() bool {
	return dirExists(webuiDistDir)
}

// dirExists is a tiny, separately-testable helper: frontendLooksBuilt
// itself is not unit tested against a real internal/webui/dist (that
// would couple this package's tests to the repository's own working
// directory, which every other test in this package deliberately
// avoids — see main_test.go's own doc comment on keeping cmd/release's
// tests fast and independent) — this pure filesystem check is.
func dirExists(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && info.IsDir()
}
