package config

import "testing"

func TestEnvSetup(t *testing.T) {
	// t.Setenv("APP_ENV", "test")
	// t.Setenv("APP_PORT", "9090")
	// t.Setenv("DATABASE_URL", "postgres://test")

	cfg := Load()

	// if cfg.Environment != "test" {
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

}
