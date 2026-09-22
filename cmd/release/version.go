package main

import (
	"fmt"

	"golang.org/x/mod/semver"
)

// validateReleaseVersion enforces this project's release-version shape:
// a fully-specified, "v"-prefixed semantic version — vMAJOR.MINOR.PATCH,
// optionally with a prerelease suffix (v1.0.0-rc.1) — and nothing looser
// or extra.
//
// golang.org/x/mod/semver is used rather than a hand-rolled regex:
// SemVer's grammar (numeric-vs-alphanumeric prerelease identifiers, no
// leading zeros, precedence rules) is easy to get subtly wrong by hand,
// and this package is the same one the go command itself uses to
// interpret module versions — small (no dependencies beyond the
// standard library), already reachable from this module's existing
// dependency graph, and authoritative, so pulling it in for this one
// purpose is a better trade than either a large third-party semver
// library or a regex this project would have to maintain the
// correctness of itself.
//
// Two extra, deliberately stricter-than-bare-SemVer rules apply on top
// of semver.IsValid:
//
//  1. Build metadata (a "+..." suffix, e.g. v1.2.3+build.5) is rejected.
//     It is valid SemVer, but this project has no use for it, and a "+"
//     in a filename is an unnecessary complication for the artifact
//     names this version feeds directly into (see target.artifactName).
//  2. The version must already be in fully-expanded canonical form —
//     v1.2.3, never the Go-module-style abbreviation v1.2 or v1. This
//     matters because whatever string is passed here is used verbatim
//     in every artifact's filename; accepting v1.2 would silently
//     produce filenames that don't say what patch version they are.
func validateReleaseVersion(version string) error {
	if !semver.IsValid(version) {
		return fmt.Errorf(
			"invalid -version %q: must be a semantic version with a leading \"v\", e.g. v1.2.3 or v1.2.3-rc.1",
			version,
		)
	}

	if semver.Build(version) != "" {
		return fmt.Errorf(
			"invalid -version %q: build metadata (a \"+...\" suffix) is not supported in release artifact names",
			version,
		)
	}

	if semver.Canonical(version) != version {
		return fmt.Errorf(
			"invalid -version %q: must fully specify MAJOR.MINOR.PATCH, e.g. v1.2.3 (not v1.2 or v1)",
			version,
		)
	}

	return nil
}
