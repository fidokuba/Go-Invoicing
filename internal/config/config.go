package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment string
	DatabaseURL string
	Port        string

	// WorkerEnabled, WorkerInterval and WorkerBatchSize (Milestone 6)
	// configure the in-process SessionCleanupWorker started by
	// cmd/api/main.go. There is only one background job today, so one set
	// of settings is enough — a second job would get its own, not a
	// generic "worker config" shared across jobs.
	WorkerEnabled   bool
	WorkerInterval  time.Duration
	WorkerBatchSize int
}

func Load() (Config, error) {
	cfg := Config{
		Environment: getEnv("APP_ENV", "development"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://go_invoicing:go_invoicing_dev@localhost:5432/go_invoicing?sslmode=disable"),
		Port:        getEnv("APP_PORT", "8080"),
	}

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

	return cfg, nil
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
