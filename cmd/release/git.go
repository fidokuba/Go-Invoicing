package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// gitHeadCommit returns the full (not abbreviated) Git SHA of HEAD — the
// commit actually being built, never derived from a branch name. The
// full SHA is preferred over a short one: for support/debugging a
// customer's exact build, an unambiguous full SHA is worth the extra
// characters (see this milestone's own report for the trade-off).
//
// This is a build-time-only concern: nothing in the running application
// ever calls this — internal/buildinfo.Commit is a plain string set at
// link time (see releaseLDFlags), and reading Git at runtime was
// explicitly ruled out when that package was designed (Milestone 11
// Part 2).
func gitHeadCommit() (string, error) {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// gitIsDirty reports whether the working tree has any uncommitted
// changes (tracked modifications, staged changes, or untracked files).
// The canonical release build does not refuse to run on a dirty tree
// (see this milestone's own explicit decision: local release builds may
// proceed from a dirty tree, clearly reported, since Part 6 owns actual
// publishing and can enforce a clean-tree requirement there) — this is
// only used to decide whether to print a warning.
func gitIsDirty() (bool, error) {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("git status --porcelain: %w", err)
	}

	return strings.TrimSpace(string(out)) != "", nil
}
