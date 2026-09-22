package main

import "testing"

func TestValidateReleaseVersion_Valid(t *testing.T) {
	for _, v := range []string{
		"v0.1.0",
		"v1.0.0",
		"v2.4.13",
		"v1.0.0-rc.1",
		"v0.0.0-test",
		"v1.2.3-alpha",
		"v1.2.3-alpha.1",
	} {
		t.Run(v, func(t *testing.T) {
			if err := validateReleaseVersion(v); err != nil {
				t.Errorf("expected %q to be valid, got error: %v", v, err)
			}
		})
	}
}

func TestValidateReleaseVersion_Invalid(t *testing.T) {
	for _, v := range []string{
		"",
		"1.0.0",      // missing leading "v"
		"v1",         // not fully specified
		"v1.0",       // not fully specified
		"vabc",       // not numeric
		"v1.0.0-",    // trailing dash, empty prerelease identifier
		"v1.0.0.0",   // too many components
		"v1.2.3+b.5", // build metadata not supported
		"latest",     // not semver at all
		"v01.2.3",    // leading zero
		" v1.2.3",    // leading whitespace
		"v1.2.3 ",    // trailing whitespace
	} {
		t.Run(v, func(t *testing.T) {
			if err := validateReleaseVersion(v); err == nil {
				t.Errorf("expected %q to be rejected, got no error", v)
			}
		})
	}
}

// TestValidateReleaseVersion_ErrorNeverEmpty is a small defensive check:
// every rejection should explain itself, since this is the message a
// developer or CI log will actually show for a typo'd -version flag.
func TestValidateReleaseVersion_ErrorNeverEmpty(t *testing.T) {
	err := validateReleaseVersion("not-a-version")
	if err == nil {
		t.Fatal("expected an error")
	}
	if err.Error() == "" {
		t.Error("expected a non-empty error message")
	}
}
