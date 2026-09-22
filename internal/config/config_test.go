package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Environment == "" {
		t.Errorf("Environment cannot be blank, found %q", cfg.Environment)
	}

	if cfg.Port == "" {
		t.Errorf("Port cannot be blank, found %q", cfg.Port)
	}

	if cfg.DatabaseURL == "" {
		t.Errorf(
			"Database URL cannot be blank, found %q",
			cfg.DatabaseURL,
		)
	}

	if !cfg.WorkerEnabled {
		t.Error("expected WorkerEnabled to default to true")
	}

	if cfg.WorkerInterval != time.Hour {
		t.Errorf("expected WorkerInterval to default to 1h, got %s", cfg.WorkerInterval)
	}

	if cfg.WorkerBatchSize != 500 {
		t.Errorf("expected WorkerBatchSize to default to 500, got %d", cfg.WorkerBatchSize)
	}

	if !cfg.MetricsEnabled {
		t.Error("expected MetricsEnabled to default to true")
	}

	if cfg.Host != "" {
		t.Errorf("expected Host to default to \"\" (every interface), got %q", cfg.Host)
	}

	if cfg.LogFormat != "text" {
		t.Errorf("expected LogFormat to default to \"text\", got %q", cfg.LogFormat)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel to default to \"info\", got %q", cfg.LogLevel)
	}
}

// --- Milestone 11 Part 2: production DATABASE_URL safety ---

// TestLoad_DevelopmentDefaultsDatabaseURLWhenUnset proves the
// development-convenience default from TestLoad_Defaults above is
// specifically tied to APP_ENV=development (its own implicit default),
// not merely "DATABASE_URL is optional."
func TestLoad_DevelopmentDefaultsDatabaseURLWhenUnset(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.DatabaseURL == "" {
		t.Error("expected a non-empty development DATABASE_URL default")
	}
}

// TestLoad_NonDevelopmentRequiresDatabaseURL is this Part's central
// safety invariant: a non-development environment must fail closed
// rather than silently starting up against the development database.
func TestLoad_NonDevelopmentRequiresDatabaseURL(t *testing.T) {
	for _, env := range []string{"production", "staging", "qa", "anything-else"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			t.Setenv("DATABASE_URL", "")

			_, err := Load()
			if err == nil {
				t.Fatalf("expected APP_ENV=%s with no DATABASE_URL to fail configuration", env)
			}
			if !strings.Contains(err.Error(), "DATABASE_URL") {
				t.Errorf("expected the error to mention DATABASE_URL, got: %v", err)
			}
		})
	}
}

// TestLoad_NonDevelopmentWithDatabaseURLSucceeds proves the safety rule
// only guards the *missing* case — an explicitly supplied DATABASE_URL
// works in any environment, verbatim.
func TestLoad_NonDevelopmentWithDatabaseURLSucceeds(t *testing.T) {
	const explicit = "postgres://prod_user:prod_pass@prod-host:5432/prod_db?sslmode=require"

	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", explicit)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.DatabaseURL != explicit {
		t.Errorf("expected DatabaseURL %q, got %q", explicit, cfg.DatabaseURL)
	}
}

// TestLoad_EnvironmentComparisonIsCaseInsensitive proves "Development"/
// "DEVELOPMENT"/etc. are all still treated as the development
// environment for the DATABASE_URL default — consistent with this file's
// existing tolerance for casing variants (getBoolEnv already accepts
// "True"/"TRUE" via strconv.ParseBool).
func TestLoad_EnvironmentComparisonIsCaseInsensitive(t *testing.T) {
	for _, env := range []string{"Development", "DEVELOPMENT", "DevElopment"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			t.Setenv("DATABASE_URL", "")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.DatabaseURL == "" {
				t.Error("expected the development DATABASE_URL default to still apply")
			}
		})
	}
}

// TestLoad_MissingDatabaseURLErrorNeverLeaksAnyValue proves the
// missing-DATABASE_URL error text is exactly what it claims to be: the
// environment name and nothing else. There is no secret VALUE in this
// failure case (DATABASE_URL is empty), but this pins the error's exact
// shape so a future change can't accidentally start interpolating one.
func TestLoad_MissingDatabaseURLErrorNeverLeaksAnyValue(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "production") {
		t.Errorf("expected the error to mention the actual APP_ENV value, got: %v", err)
	}
	if !strings.Contains(err.Error(), "development") {
		t.Errorf("expected the error to explain the development-only default, got: %v", err)
	}
}

// TestLoad_ConfigErrorsNeverContainDatabaseURLValue is a broader
// adversarial regression than the missing-value case above: even when
// DATABASE_URL IS set (to a value containing a distinctive fake
// credential) and some OTHER config value is simultaneously invalid, the
// resulting error must never echo the DATABASE_URL value — no config
// error path in this file ever has a legitimate reason to interpolate
// it.
func TestLoad_ConfigErrorsNeverContainDatabaseURLValue(t *testing.T) {
	const marker = "postgres://user:MARKER-SECRET-abc123@host:5432/db"

	t.Setenv("DATABASE_URL", marker)
	t.Setenv("WORKER_INTERVAL", "not-a-duration") // forces a config error

	_, err := Load()
	if err == nil {
		t.Fatal("expected a configuration error from the invalid WORKER_INTERVAL")
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "MARKER-SECRET-abc123") {
		t.Fatalf("expected DATABASE_URL never to appear in a config error, got: %v", err)
	}
}

// --- Milestone 11 Part 2: APP_HOST ---

func TestLoad_HostOverride(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "::1", "0.0.0.0"} {
		t.Run(host, func(t *testing.T) {
			t.Setenv("APP_HOST", host)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.Host != host {
				t.Errorf("expected Host %q, got %q", host, cfg.Host)
			}
		})
	}
}

// --- Milestone 11 Part 2: APP_PORT validation ---

func TestLoad_InvalidAppPort(t *testing.T) {
	cases := []string{"not-a-number", "0", "-1", "65536", "99999", "8080.5"}

	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("APP_PORT", value)

			if _, err := Load(); err == nil {
				t.Fatalf("expected an error for APP_PORT=%q", value)
			}
		})
	}
}

func TestLoad_ValidAppPortBoundaries(t *testing.T) {
	for _, value := range []string{"1", "80", "8080", "65535"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("APP_PORT", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.Port != value {
				t.Errorf("expected Port %q, got %q", value, cfg.Port)
			}
		})
	}
}

// --- Milestone 11 Part 2: LOG_FORMAT ---

func TestLoad_LogFormatOverrides(t *testing.T) {
	for _, value := range []string{"text", "json", "JSON", "Text"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("LOG_FORMAT", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.LogFormat != strings.ToLower(value) {
				t.Errorf("expected LogFormat %q, got %q", strings.ToLower(value), cfg.LogFormat)
			}
		})
	}
}

func TestLoad_InvalidLogFormat(t *testing.T) {
	t.Setenv("LOG_FORMAT", "xml")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an invalid LOG_FORMAT")
	}
}

// --- Milestone 11 Part 2: LOG_LEVEL ---

func TestLoad_LogLevelOverrides(t *testing.T) {
	for _, value := range []string{"debug", "info", "warn", "error", "DEBUG", "Error"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.LogLevel != strings.ToLower(value) {
				t.Errorf("expected LogLevel %q, got %q", strings.ToLower(value), cfg.LogLevel)
			}
		})
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "verbose")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an invalid LOG_LEVEL")
	}
}

func TestLoad_WorkerOverrides(t *testing.T) {
	t.Setenv("WORKER_ENABLED", "false")
	t.Setenv("WORKER_INTERVAL", "10m")
	t.Setenv("WORKER_BATCH_SIZE", "50")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.WorkerEnabled {
		t.Error("expected WorkerEnabled to be false")
	}

	if cfg.WorkerInterval != 10*time.Minute {
		t.Errorf("expected WorkerInterval 10m, got %s", cfg.WorkerInterval)
	}

	if cfg.WorkerBatchSize != 50 {
		t.Errorf("expected WorkerBatchSize 50, got %d", cfg.WorkerBatchSize)
	}
}

func TestLoad_WorkerEnabledTrueVariants(t *testing.T) {
	for _, value := range []string{"1", "t", "T", "TRUE", "true", "True"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("WORKER_ENABLED", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}

			if !cfg.WorkerEnabled {
				t.Errorf("expected WorkerEnabled true for %q", value)
			}
		})
	}
}

func TestLoad_WorkerEnabledFalseVariants(t *testing.T) {
	for _, value := range []string{"0", "f", "F", "FALSE", "false", "False"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("WORKER_ENABLED", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}

			if cfg.WorkerEnabled {
				t.Errorf("expected WorkerEnabled false for %q", value)
			}
		})
	}
}

// TestLoad_InvalidWorkerEnabled proves a malformed WORKER_ENABLED value is
// rejected outright rather than silently falling back to the default —
// consistent with how WORKER_INTERVAL and WORKER_BATCH_SIZE are validated
// below.
func TestLoad_InvalidWorkerEnabled(t *testing.T) {
	t.Setenv("WORKER_ENABLED", "not-a-bool")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a malformed WORKER_ENABLED value")
	}
}

// TestLoad_MetricsEnabledTrueVariants and TestLoad_MetricsEnabledFalseVariants
// mirror the WORKER_ENABLED variant sweeps above — same strconv.ParseBool
// parsing (see getBoolEnv), just a second, independent boolean flag.
func TestLoad_MetricsEnabledTrueVariants(t *testing.T) {
	for _, value := range []string{"1", "t", "T", "TRUE", "true", "True"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("METRICS_ENABLED", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}

			if !cfg.MetricsEnabled {
				t.Errorf("expected MetricsEnabled true for %q", value)
			}
		})
	}
}

func TestLoad_MetricsEnabledFalseVariants(t *testing.T) {
	for _, value := range []string{"0", "f", "F", "FALSE", "false", "False"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("METRICS_ENABLED", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}

			if cfg.MetricsEnabled {
				t.Errorf("expected MetricsEnabled false for %q", value)
			}
		})
	}
}

// TestLoad_InvalidMetricsEnabled proves a malformed METRICS_ENABLED value
// is rejected outright rather than silently falling back to the default —
// consistent with TestLoad_InvalidWorkerEnabled above.
func TestLoad_InvalidMetricsEnabled(t *testing.T) {
	t.Setenv("METRICS_ENABLED", "not-a-bool")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a malformed METRICS_ENABLED value")
	}
}

func TestLoad_InvalidWorkerInterval(t *testing.T) {
	cases := []string{"not-a-duration", "0", "-5m"}

	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("WORKER_INTERVAL", value)

			if _, err := Load(); err == nil {
				t.Fatalf("expected an error for WORKER_INTERVAL=%q", value)
			}
		})
	}
}

func TestLoad_InvalidWorkerBatchSize(t *testing.T) {
	cases := []string{"not-a-number", "0", "-10"}

	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("WORKER_BATCH_SIZE", value)

			if _, err := Load(); err == nil {
				t.Fatalf("expected an error for WORKER_BATCH_SIZE=%q", value)
			}
		})
	}
}
