package main

import "fmt"

// buildinfoImportPath is internal/buildinfo's full module import path —
// the exact string the Go linker's -X flag needs to find Version/Commit/
// BuildTime. Kept as one named constant, rather than repeated inline,
// so a future package rename/move only needs updating here.
const buildinfoImportPath = "go-invoicing/internal/buildinfo"

// releaseLDFlags builds the -ldflags string every canonical release
// build passes to `go build`: -s -w to strip the symbol table and DWARF
// debug info (see this package's own doc comment / the milestone report
// for why panic stack traces remain useful anyway), plus one -X per
// internal/buildinfo variable. version, commit and buildTime are
// assumed already resolved/validated by the caller — this function only
// formats them.
func releaseLDFlags(version, commit, buildTime string) string {
	return fmt.Sprintf(
		"-s -w -X %s.Version=%s -X %s.Commit=%s -X %s.BuildTime=%s",
		buildinfoImportPath, version,
		buildinfoImportPath, commit,
		buildinfoImportPath, buildTime,
	)
}
