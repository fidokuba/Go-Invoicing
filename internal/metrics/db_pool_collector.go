package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// dbPoolCollector is a scrape-time prometheus.Collector over
// pgxpool.Pool.Stat() — deliberately not a set of gauges/counters kept
// up to date by a background polling goroutine (section 15's explicit
// instruction). pgxpool already maintains every one of these numbers
// internally; polling it on a timer would only add a second, redundant
// copy that could drift from what Stat() reports at scrape time, plus a
// goroutine this package would then have to start and stop cleanly. A
// Collector's Collect method already runs exactly when something is
// actually scraping GET /metrics, which is the only time this data is
// needed.
//
// There is exactly one pgxpool.Pool in this application (see app.New's
// own doc comment on why a nil pool is even tolerated), so none of these
// metrics carry a pool-name label — see this milestone's own section 16.
type dbPoolCollector struct {
	pool *pgxpool.Pool

	connections           *prometheus.Desc
	acquiresTotal         *prometheus.Desc
	acquireDurationTotal  *prometheus.Desc
	emptyAcquiresTotal    *prometheus.Desc
	canceledAcquiresTotal *prometheus.Desc
}

func newDBPoolCollector(pool *pgxpool.Pool) *dbPoolCollector {
	return &dbPoolCollector{
		pool: pool,

		// One gauge vec with a bounded "state" label rather than five
		// separate gauge names — state is a fixed five-value enum
		// (total/acquired/idle/constructing/max), not an open-ended
		// dimension, so this is the same kind of bounded label the
		// milestone's cardinality rules explicitly allow.
		connections: prometheus.NewDesc(
			namespace+"_db_pool_connections",
			"Current number of pool connections, by state (total, acquired, idle, constructing, max).",
			[]string{"state"}, nil,
		),
		acquiresTotal: prometheus.NewDesc(
			namespace+"_db_pool_acquires_total",
			"Cumulative count of successful connection acquires from the pool.",
			nil, nil,
		),
		acquireDurationTotal: prometheus.NewDesc(
			namespace+"_db_pool_acquire_duration_seconds_total",
			"Cumulative time spent acquiring connections from the pool, in seconds.",
			nil, nil,
		),
		emptyAcquiresTotal: prometheus.NewDesc(
			namespace+"_db_pool_empty_acquires_total",
			"Cumulative count of acquires that had to wait because the pool had no idle connection available.",
			nil, nil,
		),
		canceledAcquiresTotal: prometheus.NewDesc(
			namespace+"_db_pool_canceled_acquires_total",
			"Cumulative count of acquires canceled by their caller's context before a connection became available.",
			nil, nil,
		),
	}
}

func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.acquiresTotal
	ch <- c.acquireDurationTotal
	ch <- c.emptyAcquiresTotal
	ch <- c.canceledAcquiresTotal
}

// Collect emits nothing for a nil pool (see New's own doc comment on why
// a nil pool must be tolerated here at all) rather than panicking on
// pool.Stat().
func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	if c.pool == nil {
		return
	}

	stat := c.pool.Stat()

	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stat.TotalConns()), "total")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stat.AcquiredConns()), "acquired")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stat.IdleConns()), "idle")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stat.ConstructingConns()), "constructing")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stat.MaxConns()), "max")

	ch <- prometheus.MustNewConstMetric(c.acquiresTotal, prometheus.CounterValue, float64(stat.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireDurationTotal, prometheus.CounterValue, stat.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.emptyAcquiresTotal, prometheus.CounterValue, float64(stat.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.canceledAcquiresTotal, prometheus.CounterValue, float64(stat.CanceledAcquireCount()))
}
