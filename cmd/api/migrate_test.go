package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// Milestone 13 Part 5: the `migrate` subcommand and the startup schema step.

func TestMigrateCommandRequested(t *testing.T) {
	if !migrateCommandRequested([]string{"migrate"}) {
		t.Error("expected `migrate` to be recognised")
	}
	for _, args := range [][]string{nil, {}, {"--version"}, {"serve"}, {"--migrate"}, {"x", "migrate"}} {
		if migrateCommandRequested(args) {
			t.Errorf("expected %q not to be the migrate command", args)
		}
	}
}

func TestRunMigrateCommand_ExitCodes(t *testing.T) {
	logger := newLogger(io.Discard, "text", "info")

	var migratedURL string
	if code := runMigrateCommand(logger, "postgres://db", func(url string) error { migratedURL = url; return nil }); code != 0 {
		t.Errorf("expected exit code 0 on success, got %d", code)
	}
	if migratedURL != "postgres://db" {
		t.Errorf("expected migrations to run against the configured URL, got %q", migratedURL)
	}

	var logs bytes.Buffer
	if code := runMigrateCommand(newLogger(&logs, "text", "info"), "postgres://db", func(string) error { return errors.New("boom") }); code != 1 {
		t.Errorf("expected exit code 1 on failure, got %d", code)
	}
	if !bytes.Contains(logs.Bytes(), []byte("database migration failed")) {
		t.Error("expected the failure to be logged")
	}
}

func TestPrepareSchema_MigratesOrOnlyChecks(t *testing.T) {
	logger := newLogger(io.Discard, "text", "info")

	var migrated, checked int
	migrate := func(string) error { migrated++; return nil }
	check := func(string) error { checked++; return errors.New("behind") }

	if err := prepareSchema(logger, "url", true, migrate, check); err != nil || migrated != 1 || checked != 0 {
		t.Errorf("MIGRATE_ON_STARTUP=true: expected only a migration, got migrated=%d checked=%d err=%v", migrated, checked, err)
	}

	if err := prepareSchema(logger, "url", false, migrate, check); err == nil || migrated != 1 || checked != 1 {
		t.Errorf("MIGRATE_ON_STARTUP=false: expected only a check (whose error is returned), got migrated=%d checked=%d err=%v", migrated, checked, err)
	}
}
