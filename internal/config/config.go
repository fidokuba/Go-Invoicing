package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// developmentEnvironment is the one APP_ENV value that may fall back to
// developmentDatabaseURLDefault — see resolveDatabaseURL's own doc
// comment for the production-safety rule this enables. Comparison
// against it is case-insensitive (see resolveDatabaseURL), matching this
// file's existing tolerance for casing variants elsewhere (getBoolEnv
// already accepts "true"/"True"/"TRUE" via strconv.ParseBool).
const developmentEnvironment = "development"

// developmentDatabaseURLDefault is a local, throwaway credential pair —
// the same database compose.yaml provisions for local development. It is
// safe to default to only because resolveDatabaseURL refuses to apply it
// for any APP_ENV other than developmentEnvironment.
const developmentDatabaseURLDefault = "postgres://go_invoicing:go_invoicing_dev@localhost:5432/go_invoicing?sslmode=disable"

type Config struct {
	Environment string
	DatabaseURL string

	// Host and Port (Milestone 11 Part 2 adds Host) are combined via
	// net.JoinHostPort by cmd/api/main.go — never naive string
	// concatenation — so an IPv6 Host (e.g. "::1") produces a correct
	// "[::1]:8080" listen address instead of a malformed one. Host
	// defaults to "" (every interface), preserving the exact bind
	// behaviour this application had before Host existed.
	Host string
	Port string

	// LogFormat and LogLevel (Milestone 11 Part 2) configure the one
	// *slog.Logger cmd/api/main.go constructs in its composition root —
	// see that file's own doc comment on why a small bootstrap logger
	// still exists separately for the one error path (an invalid Config
	// itself) that necessarily happens before these values are known.
	// Both are validated, bounded string enums (see getEnumEnv) rather
	// than free-form strings, so a typo fails configuration instead of
	// silently behaving like some other value.
	LogFormat string
	LogLevel  string

	// WorkerEnabled, WorkerInterval and WorkerBatchSize (Milestone 6)
	// configure the in-process SessionCleanupWorker started by
	// cmd/api/main.go. There is only one background job today, so one set
	// of settings is enough — a second job would get its own, not a
	// generic "worker config" shared across jobs.
	WorkerEnabled   bool
	WorkerInterval  time.Duration
	WorkerBatchSize int

	// MetricsEnabled (Milestone 10 Part 4) controls whether GET /metrics
	// is registered at all (see app.New's own doc comment: a nil
	// *metrics.Metrics means the route is never mounted, and every
	// metrics recording call elsewhere becomes a no-op). Defaults to
	// enabled, matching WorkerEnabled's own "on unless explicitly turned
	// off" convention: nothing this milestone exposes carries a request
	// ID, tenant/user/invoice identifier, or any other sensitive value
	// (see the metrics package's own cardinality-policy doc comment), so
	// there is no data-sensitivity reason to default it off the way, say,
	// a debug/pprof endpoint would be. Operators who genuinely need
	// /metrics unreachable (no network-level restriction available at
	// all) can still set METRICS_ENABLED=false.
	MetricsEnabled bool
}

func Load() (Config, error) {
	cfg := Config{
		Environment: getEnv("APP_ENV", developmentEnvironment),
		Host:        getEnv("APP_HOST", ""),
		Port:        getEnv("APP_PORT", "8080"),
	}

	if err := validatePort(cfg.Port); err != nil {
		return Config{}, err
	}

	databaseURL, err := resolveDatabaseURL(cfg.Environment)
	if err != nil {
		return Config{}, err
	}
	cfg.DatabaseURL = databaseURL

	logFormat, err := getEnumEnv("LOG_FORMAT", "text", "text", "json")
	if err != nil {
		return Config{}, err
	}
	cfg.LogFormat = logFormat

	logLevel, err := getEnumEnv("LOG_LEVEL", "info", "debug", "info", "warn", "error")
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = logLevel

	workerEnabled, err := getBoolEnv("WORKER_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	cfg.WorkerEnabled = workerEnabled

	workerInterval, err := getDurationEnv("WORKER_INTERVAL", time.Hour)
	if err != nil {
		return Config{}, err
	}
	if workerInterval <= 0 {
		return Config{}, fmt.Errorf("WORKER_INTERVAL must be greater than 0, got %s", workerInterval)
	}
	cfg.WorkerInterval = workerInterval

	workerBatchSize, err := getIntEnv("WORKER_BATCH_SIZE", 500)
	if err != nil {
		return Config{}, err
	}
	if workerBatchSize <= 0 {
		return Config{}, fmt.Errorf("WORKER_BATCH_SIZE must be greater than 0, got %d", workerBatchSize)
	}
	cfg.WorkerBatchSize = workerBatchSize

	metricsEnabled, err := getBoolEnv("METRICS_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	cfg.MetricsEnabled = metricsEnabled

	return cfg, nil
}

// resolveDatabaseURL implements Milestone 11 Part 2's production-safety
// rule: DATABASE_URL may be omitted only when APP_ENV is (case-
// insensitively) "development" — in which case developmentDatabaseURLDefault
// is used, the same local, throwaway database compose.yaml provisions.
// Any other APP_ENV value (production, staging, or anything else an
// operator names — deliberately not a fixed enum, so a name like
// "staging" is never specially rejected) fails closed: DATABASE_URL must
// be set explicitly, so a real deployment can never silently start up
// against the development database merely because a deployment step
// forgot to set it.
//
// The returned error mentions only the environment name, never a
// DATABASE_URL value — there is none to leak in the one failure case
// this guards against (a missing value, not a malformed one; see
// database.Migrate's own sanitizeDSNError for the malformed-value case,
// which this function never touches since it does no parsing of its
// own).
func resolveDatabaseURL(environment string) (string, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		return databaseURL, nil
	}

	if strings.EqualFold(environment, developmentEnvironment) {
		return developmentDatabaseURLDefault, nil
	}

	return "", fmt.Errorf(
		"DATABASE_URL is required when APP_ENV is not %q (got %q)",
		developmentEnvironment, environment,
	)
}

// validatePort rejects anything that isn't a usable TCP port number.
// port 0 (the OS-assigns-an-ephemeral-port convention) is deliberately
// treated as invalid here, not merely unusual: this application's own
// startup log reports the configured port as the address to reach it at
// (see cmd/api/main.go's "API server listening" event), which port 0
// would make misleading.
func validatePort(port string) error {
	parsed, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid APP_PORT %q: must be numeric", port)
	}

	if parsed < 1 || parsed > 65535 {
		return fmt.Errorf("APP_PORT must be between 1 and 65535, got %d", parsed)
	}

	return nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}

func getBoolEnv(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: %w", key, value, err)
	}

	return parsed, nil
}

func getDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, value, err)
	}

	return parsed, nil
}

func getIntEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, value, err)
	}

	return parsed, nil
}

// getEnumEnv reads key, defaulting to fallback when unset, and validates
// the resolved value (compared case-insensitively) against allowed —
// one more small, typed getter in the same style as getBoolEnv/
// getDurationEnv/getIntEnv above, rather than a generic/reflection-based
// validation framework. The returned value is always the lower-cased
// canonical form (e.g. "JSON" and "json" both resolve to "json"); a
// rejected value's error reports exactly what the operator set, never
// the lower-cased form, so copying the error text back into the
// environment variable reproduces the same failure.
func getEnumEnv(key, fallback string, allowed ...string) (string, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}

	normalized := strings.ToLower(raw)
	for _, a := range allowed {
		if normalized == a {
			return normalized, nil
		}
	}

	return "", fmt.Errorf("invalid %s %q: must be one of %v", key, raw, allowed)
}
