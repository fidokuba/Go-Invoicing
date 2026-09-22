package main

import "testing"

// TestReleaseTargets_ExactlyTheFiveCanonicalPlatforms pins the fixed
// target list itself — Milestone 11 Part 4 section 2's exact five, in
// the same order every release build and checksums.txt relies on for
// determinism.
func TestReleaseTargets_ExactlyTheFiveCanonicalPlatforms(t *testing.T) {
	want := []target{
		{goos: "linux", goarch: "amd64"},
		{goos: "linux", goarch: "arm64"},
		{goos: "windows", goarch: "amd64"},
		{goos: "darwin", goarch: "amd64"},
		{goos: "darwin", goarch: "arm64"},
	}

	got := releaseTargets()
	if len(got) != len(want) {
		t.Fatalf("expected %d targets, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestReleaseTargets_StableAcrossCalls proves the list is a fixed
// literal, not something derived from map iteration or any other
// non-deterministic source — calling it twice must yield identical
// order.
func TestReleaseTargets_StableAcrossCalls(t *testing.T) {
	first := releaseTargets()
	second := releaseTargets()

	if len(first) != len(second) {
		t.Fatalf("length differs between calls: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("target[%d] differs between calls: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestArtifactName_Convention(t *testing.T) {
	cases := []struct {
		t    target
		want string
	}{
		{target{goos: "linux", goarch: "amd64"}, "go-invoicing_v1.2.3_linux_amd64"},
		{target{goos: "linux", goarch: "arm64"}, "go-invoicing_v1.2.3_linux_arm64"},
		{target{goos: "windows", goarch: "amd64"}, "go-invoicing_v1.2.3_windows_amd64.exe"},
		{target{goos: "darwin", goarch: "amd64"}, "go-invoicing_v1.2.3_darwin_amd64"},
		{target{goos: "darwin", goarch: "arm64"}, "go-invoicing_v1.2.3_darwin_arm64"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.t.artifactName("v1.2.3"); got != tc.want {
				t.Errorf("artifactName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestArtifactName_NoSpacesOrAmbiguity proves every generated name is a
// single, unambiguous token — safe to use as a bare filename or in a
// shell command without quoting.
func TestArtifactName_NoSpacesOrAmbiguity(t *testing.T) {
	for _, tt := range releaseTargets() {
		name := tt.artifactName("v0.1.0")
		for _, r := range name {
			if r == ' ' || r == '\t' || r == '\n' {
				t.Errorf("artifact name %q contains whitespace", name)
			}
		}
	}
}

// TestArtifactName_OnlyWindowsGetsExeSuffix guards the one
// platform-specific naming rule explicitly.
func TestArtifactName_OnlyWindowsGetsExeSuffix(t *testing.T) {
	for _, tt := range releaseTargets() {
		name := tt.artifactName("v0.1.0")
		hasExe := len(name) >= 4 && name[len(name)-4:] == ".exe"
		if tt.goos == "windows" && !hasExe {
			t.Errorf("expected %q (windows) to end in .exe", name)
		}
		if tt.goos != "windows" && hasExe {
			t.Errorf("expected %q (%s) not to end in .exe", name, tt.goos)
		}
	}
}
