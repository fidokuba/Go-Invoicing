package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// requireDatabaseTestsEnv is recognized ONLY by this one test — never by
// application configuration (internal/config has no such flag, and
// never will: this is a CI-only concern, not a deployment one) — and
// never by any of the ~150 other PostgreSQL-backed tests across this
// suite, which keep their own existing "skip if DATABASE_URL is unset"
// behavior (see e.g. internal/app/app_test.go's own newTestPool)
// completely untouched. Set to exactly "true" in CI only.
const requireDatabaseTestsEnv = "REQUIRE_DATABASE_TESTS"

// TestRequireDatabaseTests_CIPreflight is Milestone 11 Part 3's guard
// against the specific way CI could go green for the wrong reason: if
// DATABASE_URL were ever missing or pointed at an unreachable database in
// the CI job definition itself, every one of this suite's real
// PostgreSQL-backed tests would silently call t.Skip and report as
// passing — CI would be green because nothing was actually verified, not
// because it was.
//
// This is deliberately the ONLY test in the whole suite whose behavior
// differs between "DATABASE_URL missing" (skip) and "database required
// but unreachable" (fail): every other DB-backed test's own skip
// convention is left completely alone, so this adds the CI safety net
// without touching (let alone redesigning) the existing integration-test
// architecture at all.
//
// It also applies migrations for real, via the exact Migrate function
// cmd/api's own startup uses — nothing else in this test suite ever
// calls Migrate (cmd/api's server startup is the only other caller), and
// `go test ./...` gives no guarantee about which package's tests run
// first, so migrations cannot be safely left to "whichever DB-backed
// test happens to run first" — they must be applied by one explicit,
// ordered step before the rest of the suite runs. See
// .github/workflows/ci.yml, which runs this exact test (via `-run
// TestRequireDatabaseTests_CIPreflight`) as its own step before the full
// suite, in both the integration and race jobs.
func TestRequireDatabaseTests_CIPreflight(t *testing.T) {
	if os.Getenv(requireDatabaseTestsEnv) != "true" {
		t.Skipf("%s is not \"true\" — this preflight only runs in CI (see README's CI section); a local run without it behaves exactly as before", requireDatabaseTestsEnv)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatalf("%s=true but DATABASE_URL is not set — every PostgreSQL-backed test in this suite would silently skip", requireDatabaseTestsEnv)
	}

	if err := Migrate(databaseURL); err != nil {
		// err is already safe to print: Migrate's own sanitizeDSNError
		// (see migrations.go) strips databaseURL out of it if the
		// underlying driver ever echoed it back verbatim.
		t.Fatalf("%s=true but migrations failed to apply: %v", requireDatabaseTestsEnv, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("%s=true but the configured DATABASE_URL could not be parsed: %v", requireDatabaseTestsEnv, err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("%s=true but the configured PostgreSQL instance is not reachable: %v", requireDatabaseTestsEnv, err)
	}
}
