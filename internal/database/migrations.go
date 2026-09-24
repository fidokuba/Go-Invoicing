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

// Concurrency (Milestone 13 Part 5): golang-migrate's PostgreSQL driver
// holds a session-level pg_advisory_lock for the whole run, so several
// processes migrating at once (e.g. instances starting together with
// MIGRATE_ON_STARTUP=true) are serialised — the first applies what's
// pending, the rest wait and then find nothing to do. Each migration
// file runs as one multi-statement Exec, i.e. one implicit transaction.
func runMigrations(databaseURL string) error {
	m, closeAll, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeAll()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// ErrSchemaNotCurrent is returned by CheckSchemaCurrent when the
// database is behind this binary's migrations, or dirty.
var ErrSchemaNotCurrent = errors.New("database schema is not up to date: run `go-invoicing migrate`")

// CheckSchemaCurrent (Milestone 13 Part 5) verifies — without changing
// anything — that the database has every migration this binary embeds
// applied and is not left dirty by a failed migration. It is what the
// server runs instead of Migrate when MIGRATE_ON_STARTUP=false, so an
// instance started before the release's `go-invoicing migrate` step
// refuses to serve rather than failing later on a missing column.
//
// A database AHEAD of this binary is accepted: that is the normal state
// of a still-running old instance during a rolling deploy, after the new
// release's (additive) migrations have been applied.
func CheckSchemaCurrent(databaseURL string) error {
	if err := checkSchemaCurrent(databaseURL); err != nil {
		return sanitizeDSNError(err, databaseURL)
	}

	return nil
}

func checkSchemaCurrent(databaseURL string) error {
	latest, err := latestEmbeddedVersion()
	if err != nil {
		return err
	}

	m, closeAll, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeAll()

	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("%w (no migrations applied; this binary needs version %d)", ErrSchemaNotCurrent, latest)
	}
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("%w (version %d is dirty: a migration failed part-way and needs manual repair)", ErrSchemaNotCurrent, version)
	}
	if version < latest {
		return fmt.Errorf("%w (database is at version %d; this binary needs version %d)", ErrSchemaNotCurrent, version, latest)
	}

	return nil
}

// latestEmbeddedVersion is the highest migration version this binary
// embeds.
func latestEmbeddedVersion() (uint, error) {
	ds, err := migratefs.New(embedMigrations, "migrations")
	if err != nil {
		return 0, fmt.Errorf("create migration source: %w", err)
	}
	defer ds.Close()

	version, err := ds.First()
	if err != nil {
		return 0, fmt.Errorf("read first migration: %w", err)
	}
	for {
		next, err := ds.Next(version)
		if err != nil {
			// os.ErrNotExist: version is the last one.
			return version, nil
		}
		version = next
	}
}

// newMigrator builds a golang-migrate instance over the embedded
// migrations and databaseURL. closeAll releases everything it opened.
func newMigrator(databaseURL string) (*migrate.Migrate, func(), error) {
	// Create a migration source from the embedded SQL files.
	ds, err := migratefs.New(embedMigrations, "migrations")
	if err != nil {
		return nil, nil, fmt.Errorf("create migration source: %w", err)
	}

	// Open a PostgreSQL connection using the supplied database URL.
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		ds.Close()
		return nil, nil, fmt.Errorf("open database connection: %w", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		db.Close()
		ds.Close()
		return nil, nil, fmt.Errorf("create postgres migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", ds, "postgres", driver)
	if err != nil {
		db.Close()
		ds.Close()
		return nil, nil, fmt.Errorf("create migration instance: %w", err)
	}

	// m.Close closes both the source and the database driver (and with
	// it db).
	return m, func() { m.Close() }, nil
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
