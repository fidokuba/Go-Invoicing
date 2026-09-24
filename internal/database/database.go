// Create the PostgreSQL connection pool.
package database

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool creates the PostgreSQL connection pool and verifies it
// can reach the database.
//
// Pool sizing (Milestone 13 Part 3) deliberately uses pgxpool's defaults —
// MaxConns = max(4, runtime.NumCPU()), MinConns 0, connections recycled
// after 1h and closed after 30m idle, health-checked every minute. That
// suits one application instance against one PostgreSQL, and no measured
// pool pressure justifies different numbers. Any of these can be set
// without a code change through DATABASE_URL's pool_* parameters (e.g.
// ?pool_max_conns=20), which pgxpool.New already honours. Sizing across
// multiple instances belongs to Milestone 13 Part 5.
func NewPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	//Tests if the pool connection is reachable by pinging the database. If the ping fails, the pool is closed and an error is returned.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}
