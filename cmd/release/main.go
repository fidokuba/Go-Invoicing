// Command release is Milestone 11 Part 4's canonical production build
// tool: it cross-compiles ./cmd/api for this project's five release
// targets (see releaseTargets), injects version/commit/build-time into
// internal/buildinfo via linker flags (see releaseLDFlags), and writes a
// SHA-256 checksums.txt alongside the resulting binaries.
//
// It is a plain Go program under cmd/, not a shell script or Makefile:
// this project's release targets include Windows and macOS, but the
// tool itself will typically run on whatever CI's Linux runner provides
// (see .github/workflows/ci.yml) as well as any contributor's own
// machine — a single `go run ./cmd/release ...` behaves identically on
// all three host platforms, where a Bash script would need a separate
// Windows story and a Makefile would add a dependency (`make`) this
// project doesn't otherwise have. It stays a small, direct program
// (flag parsing plus a handful of testable helper functions in this
// same package, mirroring cmd/api/main.go's own newLogger/parseLogLevel/
// versionRequested pattern) rather than a framework: there is exactly
// one release process to support, so there is nothing yet to generalize
// over.
//
// Usage:
//
//	go run ./cmd/release -version v1.2.3
//
// -validate-only checks -version against this package's own SemVer rule
// and exits immediately without building anything — the release workflow
// (.github/workflows/release.yml, Milestone 11 Part 6) uses exactly this
// to validate a Git tag before doing any other release work, so the tag
// validation rule is never duplicated as a second, shell-side regex.
//
// See this package's own flag definitions below for every accepted
// input, and the project README's "Release builds" and "Releases"
// sections for the full walkthrough.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release build failed:", err)
		os.Exit(1)
	}
}

// run does the actual work; separated from main so it can be exercised
// with an explicit args slice and output writer without touching
// os.Args/os.Exit (mirroring cmd/api's own runServerLifecycle
// extraction). It is not itself unit-tested with real cross-compiles —
// see this package's own *_test.go files for the pure logic that is unit
// tested, and the milestone's own report for the separate, explicit
// smoke test that actually invokes this end to end.
func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	version := fs.String("version", "", "release version, e.g. v1.2.3 (required — a plain `go build ./cmd/api` remains available for development builds)")
	commit := fs.String("commit", "", "git commit SHA to embed (default: `git rev-parse HEAD`)")
	buildTime := fs.String("build-time", "", "RFC3339 UTC build timestamp to embed (default: the current time) — pass this so CI can supply one deterministic value instead of each build minting its own")
	distDir := fs.String("dist", "dist", "output directory for release artifacts (cleared and recreated on each run)")
	validateOnly := fs.Bool("validate-only", false, "validate -version and exit without building anything — used by the release workflow (Milestone 11 Part 6) to check a Git tag using this exact validator, before any other release work happens, without duplicating the SemVer rule in shell/YAML")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *version == "" {
		return errors.New("-version is required, e.g. -version v1.2.3 (a plain `go build ./cmd/api` continues to work for development builds, using internal/buildinfo's dev/unknown defaults)")
	}
	if err := validateReleaseVersion(*version); err != nil {
		return err
	}

	if *validateOnly {
		fmt.Fprintf(out, "%s is a valid release version\n", *version)
		return nil
	}

	resolvedCommit := *commit
	if resolvedCommit == "" {
		c, err := gitHeadCommit()
		if err != nil {
			return fmt.Errorf("determine git commit (pass -commit to override): %w", err)
		}
		resolvedCommit = c
	}

	// Milestone 11 Part 4's explicit decision: a dirty working tree does
	// not fail a local release build (Part 6 owns actual publishing, and
	// can enforce a clean tree there) — it is only ever reported, never
	// silently ignored.
	if dirty, err := gitIsDirty(); err != nil {
		fmt.Fprintln(out, "warning: could not determine git working-tree status:", err)
	} else if dirty {
		fmt.Fprintln(out, "warning: git working tree has uncommitted changes — embedded commit metadata reflects HEAD, not the dirty state")
	}

	// Milestone 12: every one of the five targets below embeds whatever
	// is currently on disk at internal/webui/dist (see that package's own
	// doc comment and go:embed directive) — this tool does not, and
	// should not, invoke npm itself (see this milestone's own report for
	// why: a release build tool's job is reliable Go cross-compilation,
	// not frontend tooling). If that directory still looks like the
	// committed placeholder (no assets/ subdirectory — see
	// internal/webui/dist/index.html's own comment), every released
	// binary would ship without a real frontend. This is only ever a
	// warning, matching the dirty-tree check just above: a genuine
	// development build of cmd/release (e.g. this project's own CI smoke
	// test, which intentionally never builds the frontend — see
	// .github/workflows/ci.yml) has no reason to fail over this.
	if !frontendLooksBuilt() {
		fmt.Fprintln(out, "warning: internal/webui/dist appears to be the placeholder — the frontend may not be built; run `cd web && npm ci && npm run build` first for a real release")
	}

	resolvedBuildTime := *buildTime
	if resolvedBuildTime == "" {
		resolvedBuildTime = time.Now().UTC().Format(time.RFC3339)
	}

	if err := os.RemoveAll(*distDir); err != nil {
		return fmt.Errorf("clear %s: %w", *distDir, err)
	}
	if err := os.MkdirAll(*distDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", *distDir, err)
	}

	ldflags := releaseLDFlags(*version, resolvedCommit, resolvedBuildTime)

	targets := releaseTargets()
	entries := make([]checksumEntry, 0, len(targets))
	for _, t := range targets {
		name := t.artifactName(*version)
		fmt.Fprintf(out, "building %s...\n", name)

		outPath, err := buildTarget(t, *distDir, ldflags, *version)
		if err != nil {
			return err
		}

		info, err := os.Stat(outPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", outPath, err)
		}

		sum, err := sha256File(outPath)
		if err != nil {
			return fmt.Errorf("checksum %s: %w", outPath, err)
		}

		entries = append(entries, checksumEntry{hash: sum, filename: name})
		fmt.Fprintf(out, "  ok (%d bytes)\n", info.Size())
	}

	checksumsPath := filepath.Join(*distDir, "checksums.txt")
	if err := writeChecksums(checksumsPath, entries); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}

	fmt.Fprintf(out, "\nrelease %s: %d artifacts + checksums.txt written to %s\n", *version, len(entries), *distDir)
	fmt.Fprintf(out, "commit=%s build_time=%s\n", resolvedCommit, resolvedBuildTime)

	return nil
}
