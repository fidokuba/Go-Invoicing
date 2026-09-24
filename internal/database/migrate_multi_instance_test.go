package database

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Milestone 13 Part 5: migrating an empty schema concurrently (several
// instances starting at once) and CheckSchemaCurrent. Each test gets a
// private, initially empty PostgreSQL schema via search_path, so it never
// disturbs the shared test database's own migrated schema.
func isolatedSchemaURL(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := "m135_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	return parsed.String(), admin
}

func TestMigrate_ConcurrentStartupsAreSerialised(t *testing.T) {
	schemaURL, _ := isolatedSchemaURL(t)

	const instances = 4
	errs := make([]error, instances)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = Migrate(schemaURL)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("instance %d: migrate failed: %v", i, err)
		}
	}
	if err := CheckSchemaCurrent(schemaURL); err != nil {
		t.Errorf("expected a fully migrated, clean schema, got %v", err)
	}
}

func TestCheckSchemaCurrent(t *testing.T) {
	schemaURL, admin := isolatedSchemaURL(t)
	schema := schemaFromURL(t, schemaURL)
	ctx := context.Background()

	if err := CheckSchemaCurrent(schemaURL); !errors.Is(err, ErrSchemaNotCurrent) {
		t.Fatalf("empty schema: expected ErrSchemaNotCurrent, got %v", err)
	}

	if err := Migrate(schemaURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := CheckSchemaCurrent(schemaURL); err != nil {
		t.Fatalf("migrated schema: expected no error, got %v", err)
	}

	// Behind this binary (as if its newest migration hadn't been applied).
	if _, err := admin.Exec(ctx, "UPDATE "+schema+".schema_migrations SET version = version - 1"); err != nil {
		t.Fatalf("simulate an older schema: %v", err)
	}
	if err := CheckSchemaCurrent(schemaURL); !errors.Is(err, ErrSchemaNotCurrent) {
		t.Errorf("schema behind: expected ErrSchemaNotCurrent, got %v", err)
	}

	// Ahead of this binary: an old instance during a rolling deploy.
	if _, err := admin.Exec(ctx, "UPDATE "+schema+".schema_migrations SET version = version + 100"); err != nil {
		t.Fatalf("simulate a newer schema: %v", err)
	}
	if err := CheckSchemaCurrent(schemaURL); err != nil {
		t.Errorf("schema ahead: expected no error, got %v", err)
	}

	// Dirty: a migration failed part-way.
	if _, err := admin.Exec(ctx, "UPDATE "+schema+".schema_migrations SET dirty = true"); err != nil {
		t.Fatalf("simulate a dirty schema: %v", err)
	}
	if err := CheckSchemaCurrent(schemaURL); !errors.Is(err, ErrSchemaNotCurrent) {
		t.Errorf("dirty schema: expected ErrSchemaNotCurrent, got %v", err)
	}
}

func schemaFromURL(t *testing.T, schemaURL string) string {
	t.Helper()

	parsed, err := url.Parse(schemaURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	return parsed.Query().Get("search_path")
}
