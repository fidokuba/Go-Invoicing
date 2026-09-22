// Package buildinfo holds this binary's version/commit/build-time
// metadata (Milestone 11 Part 2). Every value is a plain package-level
// string *variable* — never a const — specifically so a release build
// can override it at link time via:
//
//	go build -ldflags "\
//	  -X go-invoicing/internal/buildinfo.Version=v1.2.3 \
//	  -X go-invoicing/internal/buildinfo.Commit=abc1234 \
//	  -X go-invoicing/internal/buildinfo.BuildTime=2026-01-01T00:00:00Z" \
//	  ./cmd/api
//
// The Go linker's -X flag only works against package-level string vars,
// which is the whole reason this tiny package exists rather than these
// three values living as unexported fields in cmd/api/main.go: main is
// the composition root, not a linker target, and internal/metrics also
// needs these same values for its build-info gauge (see that package's
// own New) — a single authoritative source avoids either threading them
// through as extra constructor parameters everywhere or letting two
// packages disagree.
//
// Nothing in this package reads Git or any file at runtime — an
// uninjected build (a plain `go build`/`go run`/`go test`, exactly what
// every test and local development already does) gets the safe,
// unambiguous placeholders below instead. Milestone 11 Part 4 owns
// actually wiring CI to pass the -ldflags above; this package only
// defines where they land.
package buildinfo

import "fmt"

var (
	// Version is the released semantic version (e.g. "v1.2.3"), or "dev"
	// for a build that wasn't produced by the release pipeline.
	Version = "dev"

	// Commit is the short Git commit SHA the binary was built from, or
	// "unknown" for a build that wasn't produced by the release
	// pipeline.
	Commit = "unknown"

	// BuildTime is the RFC 3339 UTC timestamp the binary was built at,
	// or "unknown" for a build that wasn't produced by the release
	// pipeline.
	BuildTime = "unknown"
)

// String is the single human-readable summary line both `--version`
// (cmd/api/main.go) and the startup log's build-metadata event are
// built from, so the two can never drift apart in format.
func String() string {
	return fmt.Sprintf("go-invoicing %s (commit %s, built %s)", Version, Commit, BuildTime)
}
