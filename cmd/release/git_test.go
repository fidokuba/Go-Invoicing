package main

import (
	"regexp"
	"testing"
)

var fullSHARE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// TestGitHeadCommit_ReturnsFullSHA runs against this actual repository's
// real HEAD (there is no fake git repo fixture here — this package's own
// module checkout is git-controlled, which is exactly the environment
// this function is meant to run in) and proves it returns a full,
// 40-character SHA — never an abbreviated one, and never something
// derived from a branch name.
func TestGitHeadCommit_ReturnsFullSHA(t *testing.T) {
	commit, err := gitHeadCommit()
	if err != nil {
		t.Fatalf("gitHeadCommit: %v", err)
	}

	if !fullSHARE.MatchString(commit) {
		t.Errorf("expected a 40-character hex SHA, got %q", commit)
	}
}

// TestGitIsDirty_RunsWithoutError just proves the command itself is
// well-formed and executes cleanly here — it deliberately does not
// assert true or false, since whether this checkout happens to be dirty
// while the test runs is not something this test should depend on.
func TestGitIsDirty_RunsWithoutError(t *testing.T) {
	if _, err := gitIsDirty(); err != nil {
		t.Fatalf("gitIsDirty: %v", err)
	}
}
