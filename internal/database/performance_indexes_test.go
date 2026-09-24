package database

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPerformanceIndexes_Exist guards migration 000017 (Milestone 13
// Part 3): each index was added because a measured EXPLAIN (ANALYZE)
// showed the query scanning the whole table — every tenant's rows —
// without it. Pinning the exact definitions stops a later migration from
// silently dropping or reshaping one. It asserts presence, not planner
// choices, which depend on table statistics.
func TestPerformanceIndexes_Exist(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	if err := Migrate(databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	defer pool.Close()

	want := map[string]string{
		"payments_invoice_id_idx":              "CREATE INDEX payments_invoice_id_idx ON public.payments USING btree (invoice_id)",
		"invoice_lines_invoice_id_idx":         "CREATE INDEX invoice_lines_invoice_id_idx ON public.invoice_lines USING btree (invoice_id)",
		"invoices_organisation_issue_date_idx": "CREATE INDEX invoices_organisation_issue_date_idx ON public.invoices USING btree (organisation_id, issue_date DESC, id)",
		"customers_organisation_name_idx":      "CREATE INDEX customers_organisation_name_idx ON public.customers USING btree (organisation_id, name, id)",
	}

	for name, definition := range want {
		var got string
		if err := pool.QueryRow(ctx, "SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1", name).Scan(&got); err != nil {
			t.Errorf("index %s: %v", name, err)
			continue
		}
		if got != definition {
			t.Errorf("index %s:\n got  %s\n want %s", name, got, definition)
		}
	}
}
