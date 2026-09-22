package main

import "fmt"

// target is one canonical release platform (Milestone 11 Part 4 section
// 2). This fixed, ordered list — not a filesystem glob, not something
// derived from the host's own GOOS/GOARCH — is what makes both the
// release build and its checksums.txt deterministic: every run iterates
// exactly these five entries in exactly this order.
type target struct {
	goos   string
	goarch string
}

// releaseTargets returns the five canonical release platforms, in the
// fixed order every release build/checksum file uses. Deliberately no
// 32-bit or otherwise unproven architecture: these are exactly the five
// targets Milestone 11 Part 1's cross-compilation audit proved this
// CGO-free application builds cleanly for.
func releaseTargets() []target {
	return []target{
		{goos: "linux", goarch: "amd64"},
		{goos: "linux", goarch: "arm64"},
		{goos: "windows", goarch: "amd64"},
		{goos: "darwin", goarch: "amd64"},
		{goos: "darwin", goarch: "arm64"},
	}
}

// artifactName is this project's one release-artifact naming convention
// — go-invoicing_<version>_<os>_<arch>, with a ".exe" suffix on Windows
// only. version is assumed already validated (see validateReleaseVersion)
// — this function does not re-validate it.
func (t target) artifactName(version string) string {
	name := fmt.Sprintf("go-invoicing_%s_%s_%s", version, t.goos, t.goarch)
	if t.goos == "windows" {
		name += ".exe"
	}

	return name
}
