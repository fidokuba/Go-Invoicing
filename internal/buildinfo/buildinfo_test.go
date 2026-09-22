package buildinfo

import "testing"

// TestDefaults_AreSafePlaceholders proves an uninjected build (a plain
// `go build`/`go run`/`go test`) never has empty or nil-ish metadata —
// every field is an unambiguous, safe placeholder instead.
func TestDefaults_AreSafePlaceholders(t *testing.T) {
	if Version != "dev" {
		t.Errorf("expected default Version %q, got %q", "dev", Version)
	}
	if Commit != "unknown" {
		t.Errorf("expected default Commit %q, got %q", "unknown", Commit)
	}
	if BuildTime != "unknown" {
		t.Errorf("expected default BuildTime %q, got %q", "unknown", BuildTime)
	}
}

// TestString_ReflectsCurrentVarValues proves String() always reports
// whatever these package vars currently hold — the property the linker's
// -X flag relies on (it overwrites the var directly; String must read it
// fresh, not have captured a value at init time).
func TestString_ReflectsCurrentVarValues(t *testing.T) {
	originalVersion, originalCommit, originalBuildTime := Version, Commit, BuildTime
	t.Cleanup(func() { Version, Commit, BuildTime = originalVersion, originalCommit, originalBuildTime })

	Version = "v1.2.3"
	Commit = "abc1234"
	BuildTime = "2026-01-01T00:00:00Z"

	got := String()
	want := "go-invoicing v1.2.3 (commit abc1234, built 2026-01-01T00:00:00Z)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestString_DefaultFormat pins the exact placeholder-value output, since
// --version's own output (cmd/api/main.go) and this milestone's own
// report both quote this format.
func TestString_DefaultFormat(t *testing.T) {
	got := String()
	want := "go-invoicing dev (commit unknown, built unknown)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
