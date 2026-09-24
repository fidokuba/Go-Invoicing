// Package metrics is Milestone 10 Part 4's small, application-owned
// Prometheus surface. It is deliberately not a generic "telemetry"
// abstraction: Metrics is a concrete struct holding this application's own
// collectors, there is no interface for it to satisfy (nothing else needs
// a second implementation), and every collector it owns lives on its own
// application-owned *prometheus.Registry rather than the package-level
// global DefaultRegisterer — see New's own doc comment for why.
//
// Every metric this package defines uses the go_invoicing_ namespace and
// carries only bounded labels (HTTP method, matched route pattern, status
// code, a fixed worker/result enum) — never a request ID, tenant/user/
// invoice identifier, raw path, query string, or error string. See
// ObserveHTTPRequest, RecordWorkerRun/RecordWorkerFailure and
// RecordPDFGeneration for where each label actually comes from.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go-invoicing/internal/buildinfo"
)

// namespace prefixes every metric this package registers, per Prometheus
// naming convention (a single, consistent application namespace rather
// than a mix of ad hoc names).
const namespace = "go_invoicing"

// Metrics holds every Prometheus collector this application exposes,
// registered once against its own private registry (see New). A nil
// *Metrics is valid and safe to call every method on — each one is a
// no-op — so every call site below can unconditionally pass whatever
// New returned (or nil, when metrics are disabled: see config.Config
// .MetricsEnabled) without an "if enabled" check scattered through
// unrelated business code.
type Metrics struct {
	registry *prometheus.Registry

	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec

	workerRunsTotal            prometheus.Counter
	workerFailuresTotal        prometheus.Counter
	workerSessionsDeletedTotal prometheus.Counter
	workerRunDuration          prometheus.Histogram

	pdfGenerationsTotal   *prometheus.CounterVec
	pdfGenerationDuration prometheus.Histogram

	paymentIdempotencyTotal *prometheus.CounterVec

	rateLimitedTotal *prometheus.CounterVec
}

// The only limiter label values RecordRateLimited accepts (Milestone 13
// Part 4).
const (
	RateLimiterLogin    = "login"
	RateLimiterRegister = "register"
	RateLimiterPDF      = "pdf"
)

// The only outcome label values RecordPaymentIdempotency accepts
// (Milestone 13 Part 1).
const (
	PaymentIdempotencyCreated  = "created"
	PaymentIdempotencyReplayed = "replayed"
	PaymentIdempotencyConflict = "conflict"
)

// New builds a Metrics with its own private *prometheus.Registry — never
// the package-level default/global registry. An application-owned
// registry is preferred here (over prometheus.DefaultRegisterer) for
// three concrete reasons specific to this codebase: (1) every other
// constructor in this project (App.New, admin.NewSessionCleanupWorker,
// invoice.NewInvoicePDFService, ...) already takes its dependencies
// explicitly rather than reaching for package-level state, so a global
// registry would be the one exception; (2) tests construct more than one
// App/worker/PDF-service instance in the same process (see
// internal/app's own table-driven suites) — registering the same
// collector names against the shared global registry twice would panic
// with a duplicate-registration error the moment a second test called
// New, whereas two independent Metrics values here are simply
// independent; (3) it keeps GET /metrics's exposition scoped to exactly
// what this package registers, with no risk of an unrelated package
// having quietly registered something onto the global registry too.
//
// pool may be nil (mirroring app.New's own tolerance of a nil
// *pgxpool.Pool for route-table-only construction): the pool-stat
// collector's Collect simply emits nothing for a nil pool rather than
// panicking — see dbPoolCollector.
func New(pool *pgxpool.Pool) *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,

		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests, labeled by method, matched route pattern, and status code.",
		}, []string{"method", "route", "status"}),

		// Standard Prometheus histogram semantics (client-library default
		// buckets, no custom bucket boundaries): this API only ever
		// exchanges small JSON payloads and one synchronous PDF response
		// (see cmd/api/main.go's own server-timeout comments), so nothing
		// about this workload has yet demonstrated a concrete need for
		// buckets tuned away from the library default.
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds, labeled by method and matched route pattern.",
		}, []string{"method", "route"}),

		// One SessionCleanupWorker "run" is one call to runIteration —
		// the immediate pass Run performs at startup, or the pass
		// triggered by one ticker tick — regardless of how many
		// DeleteExpired batches that pass drains internally. See
		// admin.SessionCleanupWorker.runIteration's own doc comment for
		// the batch-draining detail this metric deliberately does not
		// re-expose as a separate, easily-confused counter.
		workerRunsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "session_cleanup_runs_total",
			Help:      "Total number of session cleanup worker runs (one scheduled iteration each, regardless of how many batches it drained).",
		}),
		// Excludes a run that ended because ctx was cancelled for
		// ordinary shutdown — see isShutdownCancellation in
		// session_cleanup_worker.go. Only a genuine DeleteExpired error
		// counts here.
		workerFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "session_cleanup_failures_total",
			Help:      "Total number of session cleanup runs that failed with a genuine error (graceful shutdown cancellation is not counted).",
		}),
		workerSessionsDeletedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "session_cleanup_sessions_deleted_total",
			Help:      "Total number of expired/revoked sessions deleted by the session cleanup worker, summed across every batch of every run.",
		}),
		workerRunDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "session_cleanup_run_duration_seconds",
			Help:      "Duration of one session cleanup worker run (all batches it drained), in seconds.",
		}),

		// result is a fixed two-value enum ("success"/"error"), never a
		// raw error string — see RecordPDFGeneration.
		pdfGenerationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "pdf_generations_total",
			Help:      "Total number of invoice PDF generation attempts, labeled by result (success or error).",
		}, []string{"result"}),
		// Measures InvoicePDFService.Generate itself (data assembly +
		// rendering) — not the full HTTP request, which
		// http_request_duration_seconds already covers for
		// GET /invoices/{id}/pdf specifically, and which would also
		// include HTTP-layer overhead (auth, routing, header writes)
		// that isn't PDF-specific work.
		pdfGenerationDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "pdf_generation_duration_seconds",
			Help:      "Duration of invoice PDF generation (data assembly and rendering, excluding HTTP overhead), in seconds.",
		}),
		// outcome is a fixed three-value enum (created/replayed/conflict)
		// — never the idempotency key, request hash, invoice ID or any
		// payment field. See RecordPaymentIdempotency.
		paymentIdempotencyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "payment_idempotency_total",
			Help:      "Total number of keyed payment-creation requests reaching an idempotency decision, labeled by outcome (created, replayed or conflict).",
		}, []string{"outcome"}),
		// limiter is a fixed three-value enum (login/register/pdf) — never
		// a client address, user ID or route parameter.
		rateLimitedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "rate_limited_requests_total",
			Help:      "Total number of requests rejected with 429 by an in-process rate limiter, labeled by limiter (login, register or pdf).",
		}, []string{"limiter"}),
	}

	// buildInfo (Milestone 11 Part 2 — deferred by Milestone 10 Part 4
	// until build metadata actually existed) is a set-once gauge, always
	// 1: its only purpose is carrying version/commit as labels for a
	// PromQL join against other series, the conventional Prometheus
	// "info metric" pattern. Labeled by version and commit only —
	// build_time deliberately excluded: unlike version/commit (bounded
	// per released binary), a rebuild of the identical commit still gets
	// a new build_time, which would mint a fresh time series on every
	// redeploy for no operational value PromQL could actually use.
	// build_time remains available via buildinfo.String() in the startup
	// log and `--version` output instead. It is set once here, at
	// construction time, from the same authoritative source --version
	// and the startup log both read (internal/buildinfo) — there is no
	// public RecordX method for it, since nothing about it ever changes
	// for the life of one running binary.
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Always 1; labeled by the running binary's version and commit (see internal/buildinfo).",
	}, []string{"version", "commit"})
	buildInfo.WithLabelValues(buildinfo.Version, buildinfo.Commit).Set(1)

	registry.MustRegister(
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.workerRunsTotal,
		m.workerFailuresTotal,
		m.workerSessionsDeletedTotal,
		m.workerRunDuration,
		m.pdfGenerationsTotal,
		m.pdfGenerationDuration,
		m.paymentIdempotencyTotal,
		m.rateLimitedTotal,
		buildInfo,
		newDBPoolCollector(pool),
		// Standard Go runtime and process collectors (GC pauses, heap,
		// goroutine count, open file descriptors, RSS, CPU seconds, ...).
		// These are the client library's own well-established collectors
		// — Part 4 deliberately does not hand-roll any equivalent of its
		// own (section 22 of this milestone).
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// Handler returns the promhttp exposition handler for this Metrics'
// private registry — the standard Prometheus text exposition format,
// never wrapped in this API's own JSON error envelope (a metrics scrape
// is not a business API response).
func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}

	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ObserveHTTPRequest records one completed HTTP request. method and
// route must both already be bounded values: method is one of the small,
// fixed set of HTTP methods this application registers routes for (or
// "OTHER" — see httpx.normalizeHTTPMethod, which every call site already
// normalizes through before this is ever called, since raw client input
// is otherwise unbounded), and route is the matched net/http.ServeMux
// pattern's path portion only (e.g. "/api/v1/invoices/{id}", method
// prefix already stripped) or the fixed "unmatched" fallback — never a
// raw request path or path parameter value. See httpx.requestRoute,
// which is the sole producer of the route value every call site passes
// in.
func (m *Metrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}

	statusLabel := strconv.Itoa(status)
	m.httpRequestsTotal.WithLabelValues(method, route, statusLabel).Inc()
	m.httpRequestDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}

// RecordWorkerRun records one completed SessionCleanupWorker run (see the
// workerRunsTotal field doc comment above for exactly what counts as one
// run).
func (m *Metrics) RecordWorkerRun(duration time.Duration) {
	if m == nil {
		return
	}

	m.workerRunsTotal.Inc()
	m.workerRunDuration.Observe(duration.Seconds())
}

// RecordWorkerFailure records one run that ended in a genuine error
// (never a graceful-shutdown cancellation — see isShutdownCancellation in
// session_cleanup_worker.go, the only caller of this method).
func (m *Metrics) RecordWorkerFailure() {
	if m == nil {
		return
	}

	m.workerFailuresTotal.Inc()
}

// RecordSessionsDeleted adds count to the cumulative sessions-deleted
// total. count is DeleteExpired's own returned row count for one batch —
// never a computed/estimated value.
func (m *Metrics) RecordSessionsDeleted(count int64) {
	if m == nil || count <= 0 {
		return
	}

	m.workerSessionsDeletedTotal.Add(float64(count))
}

// RecordPDFGeneration records one InvoicePDFService.Generate call. result
// must be exactly "success" or "error" — a fixed, bounded enum, never
// err.Error() or any value derived from invoice/customer/organisation
// data.
func (m *Metrics) RecordPDFGeneration(result string, duration time.Duration) {
	if m == nil {
		return
	}

	m.pdfGenerationsTotal.WithLabelValues(result).Inc()
	m.pdfGenerationDuration.Observe(duration.Seconds())
}

// RecordPaymentIdempotency records the idempotency outcome of one
// payment-creation request that reached InvoiceService's key lookup and
// completed: a newly created payment, a replay of an earlier one, or a
// key reused with a different request. outcome must be one of the
// PaymentIdempotency* constants; anything else is ignored rather than
// becoming a new label value.
func (m *Metrics) RecordPaymentIdempotency(outcome string) {
	if m == nil {
		return
	}

	switch outcome {
	case PaymentIdempotencyCreated, PaymentIdempotencyReplayed, PaymentIdempotencyConflict:
		m.paymentIdempotencyTotal.WithLabelValues(outcome).Inc()
	}
}

// RecordRateLimited counts one request rejected with 429 by the named
// limiter, which must be one of the RateLimiter* constants; anything else
// is ignored rather than becoming a new label value.
func (m *Metrics) RecordRateLimited(limiter string) {
	if m == nil {
		return
	}

	switch limiter {
	case RateLimiterLogin, RateLimiterRegister, RateLimiterPDF:
		m.rateLimitedTotal.WithLabelValues(limiter).Inc()
	}
}
