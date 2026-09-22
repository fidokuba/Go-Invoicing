package config

import (
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
