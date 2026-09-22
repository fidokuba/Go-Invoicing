package metrics

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newUnconnectedPool builds a real *pgxpool.Pool that has never actually
// connected to anything: pgxpool.New only parses the connection string
// and initializes internal bookkeeping — with the client library's own
// default MinConns of 0, it creates no connections up front, and nothing
// in this test ever calls Acquire/Ping to force one. This is enough to
// exercise dbPoolCollector.Collect's real pool.Stat() path (all zero
// values, but a real *pgxpool.Stat, not a nil pool short-circuit) without
// requiring a live DATABASE_URL — proportionate coverage per section 39's
// own guidance not to build a large mocking layer solely for this.
func newUnconnectedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db?sslmode=disable")
	if err != nil {
		t.Fatalf("construct pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// TestDBPoolCollector_NilPoolEmitsNoSamples proves the collector never
// panics for a nil pool and simply contributes no series — this is the
// case app.New's own tolerance of a nil *pgxpool.Pool relies on (see its
// doc comment), and it's also exactly what every test in this project
// that constructs an App/Metrics with no live database exercises.
func TestDBPoolCollector_NilPoolEmitsNoSamples(t *testing.T) {
	m := New(nil)

	body := scrape(t, m)
	if strings.Contains(body, "go_invoicing_db_pool_") {
		t.Fatalf("expected no db_pool samples for a nil pool, got:\n%s", body)
	}
}

// TestDBPoolCollector_RealPoolEmitsScrapeTimeGauges proves a real (if
// unconnected) pool's dbPoolCollector.Collect actually reaches
// pool.Stat() and emits the documented state/counter samples, with no
// query, table, tenant, or other high-cardinality label — only the fixed
// five-value "state" enum this collector defines.
func TestDBPoolCollector_RealPoolEmitsScrapeTimeGauges(t *testing.T) {
	pool := newUnconnectedPool(t)
	m := New(pool)

	body := scrape(t, m)

	for _, state := range []string{"total", "acquired", "idle", "constructing", "max"} {
		want := `go_invoicing_db_pool_connections{state="` + state + `"} `
		if !strings.Contains(body, want) {
			t.Errorf("expected a db_pool_connections sample for state %q, got:\n%s", state, body)
		}
	}

	for _, name := range []string{
		"go_invoicing_db_pool_acquires_total",
		"go_invoicing_db_pool_acquire_duration_seconds_total",
		"go_invoicing_db_pool_empty_acquires_total",
		"go_invoicing_db_pool_canceled_acquires_total",
	} {
		if !strings.Contains(body, name+" ") {
			t.Errorf("expected a %s sample, got:\n%s", name, body)
		}
	}
}
