package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"embed"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	migratefs "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// Migrate applies any pending embedded SQL migrations against databaseURL.
// Its returned error is eventually logged verbatim by cmd/api/main.go
// (there is no client request in play at startup, so there is no
// WriteInternalError-style boundary to redact at) — see sanitizeDSNError's
// own doc comment for the one concrete leak path that guards against.
func Migrate(databaseURL string) error {
	if err := runMigrations(databaseURL); err != nil {
		return sanitizeDSNError(err, databaseURL)
	}

	return nil
}

func runMigrations(databaseURL string) error {
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

// sanitizeDSNError strips databaseURL (credentials included) out of err's
// message text if it appears there verbatim, replacing it with a fixed,
// safe placeholder. This guards one concrete, reproduced leak path: when
// databaseURL is a malformed postgres:// URL (e.g. a bad port), lib/pq's
// DSN parsing surfaces a *url.Error, whose own Error() method
// unconditionally embeds the exact string it failed to parse — which is
// the full DSN, password included — completely independent of anything
// this application does. Ordinary connection failures (wrong host,
// connection refused, bad password rejected by the server, ...) already
// come back without the DSN — pgx and lib/pq both redact it in every
// other error path this was tested against — so this only ever fires for
// the one malformed-DSN edge case, and databaseURL (an operator-supplied
// startup value, never a value this function receives from a request) is
// the only string it ever redacts; this is not a general secret-scanning
// pass over arbitrary error text.
func sanitizeDSNError(err error, databaseURL string) error {
	if err == nil || databaseURL == "" || !strings.Contains(err.Error(), databaseURL) {
		return err
	}

	return errors.New(strings.ReplaceAll(err.Error(), databaseURL, "[redacted database URL]"))
}
