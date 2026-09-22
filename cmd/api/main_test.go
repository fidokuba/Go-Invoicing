package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// --- versionRequested ---

func TestVersionRequested_RecognizesBothDashForms(t *testing.T) {
	for _, args := range [][]string{
		{"--version"},
		{"-version"},
		{"serve", "--version"}, // anywhere in argv, not just args[0]
	} {
		if !versionRequested(args) {
			t.Errorf("expected versionRequested(%v) to be true", args)
		}
	}
}

func TestVersionRequested_FalseForEverythingElse(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{},
		{"serve"},
		{"--help"},
		{"version"}, // no leading dash — not recognized, by design
	} {
		if versionRequested(args) {
			t.Errorf("expected versionRequested(%v) to be false", args)
		}
	}
}

// --- newLogger / parseLogLevel ---

func TestParseLogLevel_MapsKnownValues(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}

	for level, want := range cases {
		if got := parseLogLevel(level); got != want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", level, got, want)
		}
	}
}

func TestParseLogLevel_UnknownDefaultsToInfo(t *testing.T) {
	if got := parseLogLevel("nonsense"); got != slog.LevelInfo {
		t.Errorf("expected unknown level to default to Info, got %v", got)
	}
}

// TestNewLogger_TextFormatIsHumanReadable proves the "text" branch
// produces slog's ordinary key=value text output — the same shape
// cmd/api always used before LOG_FORMAT existed.
func TestNewLogger_TextFormatIsHumanReadable(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "text", "info")

	logger.Info("hello", "key", "value")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") {
		t.Fatalf("expected text-format output, got: %s", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Fatalf("expected the structured field in text output, got: %s", out)
	}
}

// TestNewLogger_JSONFormatIsValidJSON proves the "json" branch produces
// genuinely parseable JSON with the expected fields — not just something
// that looks JSON-ish.
func TestNewLogger_JSONFormatIsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "json", "info")

	logger.Info("hello", "key", "value")

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON output, got %q: %v", buf.String(), err)
	}
	if decoded["msg"] != "hello" {
		t.Errorf("expected msg=hello, got %v", decoded["msg"])
	}
	if decoded["key"] != "value" {
		t.Errorf("expected key=value, got %v", decoded["key"])
	}
}

// TestNewLogger_LevelFiltersBelowConfiguredMinimum proves the configured
// LOG_LEVEL actually suppresses lower-severity records, not just labels
// them.
func TestNewLogger_LevelFiltersBelowConfiguredMinimum(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "text", "warn")

	logger.Info("should be suppressed")
	logger.Warn("should appear")

	out := buf.String()
	if strings.Contains(out, "should be suppressed") {
		t.Fatalf("expected INFO to be suppressed at LOG_LEVEL=warn, got: %s", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Fatalf("expected WARN to appear at LOG_LEVEL=warn, got: %s", out)
	}
}

// TestNewLogger_UnknownFormatFallsBackToText proves newLogger never
// panics or silently produces JSON for a format value outside "json" —
// defense in depth alongside config.Load's own validation, which is the
// only thing that should ever actually reject an unknown LOG_FORMAT.
func TestNewLogger_UnknownFormatFallsBackToText(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "not-a-format", "info")

	logger.Info("hello")

	if !strings.Contains(buf.String(), "msg=hello") {
		t.Fatalf("expected text-format fallback output, got: %s", buf.String())
	}
}
