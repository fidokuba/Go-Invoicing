package database

import (
	"database/sql"
	"errors"
	"fmt"

	"embed"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	migratefs "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

func Migrate(databaseURL string) error {
	// Create a migration source from the embedded SQL files.
	ds, err := migratefs.New(embedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}
	defer ds.Close()

	// Open a PostgreSQL connection using the supplied database URL.
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("open database connection: %w", err)
	}
	defer db.Close()

	// Build the PostgreSQL migration driver for golang-migrate.
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create postgres migration driver: %w", err)
	}

	// Create the migration instance using the embedded filesystem source.
	m, err := migrate.NewWithInstance("iofs", ds, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migration instance: %w", err)
	}
	defer m.Close()

	// Apply any pending migrations. No-op when the database is already up to date.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
