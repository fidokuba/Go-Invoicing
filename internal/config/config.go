package config

import "os"

type Config struct {
	Environment string
	DatabaseURL string
	Port string
}

func Load() Config {
	return Config{
		Environment: getEnv("APP_ENV", "development"),
		DatabaseURL: getEnv("DATABASE_URL", "localhost:5432"),
		Port: getEnv("APP_PORT", "8080"),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}
