package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-invoicing/internal/buildinfo"
)

// scrape renders m's exposition body as a string for substring
// assertions — deliberately not a full-output equality assertion (this
// milestone's own section 38 guidance: avoid asserting the entire
// exposition unnecessarily).
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape /metrics: expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	return recorder.Body.String()
}

// TestNew_MultipleInstancesDoNotPanic proves two independent Metrics
// instances can be constructed in the same process without a duplicate-
// registration panic — section 29's explicit requirement, and the
// concrete reason this package prefers an application-owned
// *prometheus.Registry over the package-level global one (see New's own
// doc comment).
func TestNew_MultipleInstancesDoNotPanic(t *testing.T) {
	first := New(nil)
	second := New(nil)

	first.ObserveHTTPRequest("GET", "/x", 200, time.Millisecond)
	second.ObserveHTTPRequest("GET", "/y", 200, time.Millisecond)

	firstBody := scrape(t, first)
	secondBody := scrape(t, second)

	if strings.Contains(firstBody, `route="/y"`) {
		t.Error("expected the first registry not to see the second instance's observations")
	}
	if strings.Contains(secondBody, `route="/x"`) {
		t.Error("expected the second registry not to see the first instance's observations")
	}
}

// TestHandler_NilMetricsReturns404 proves a nil *Metrics (metrics
// disabled) never panics when scraped and behaves like an absent route —
// consistent with app.go never mounting GET /metrics at all in that case;
// this is Handler()'s own fallback for any other direct caller.
func TestHandler_NilMetricsReturns404(t *testing.T) {
	var m *Metrics

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d for a nil Metrics, got %d", http.StatusNotFound, recorder.Code)
	}
}

// TestNilMetrics_EveryMethodIsANoOp proves every recording method on a
// nil *Metrics is safe to call — the property every other package in
// this application (httpx, admin, invoice) relies on to pass m straight
// through without an "if enabled" branch at each call site.
func TestNilMetrics_EveryMethodIsANoOp(t *testing.T) {
	var m *Metrics

	m.ObserveHTTPRequest("GET", "/x", 200, time.Millisecond)
	m.RecordWorkerRun(time.Millisecond)
	m.RecordWorkerFailure()
	m.RecordSessionsDeleted(5)
	m.RecordPDFGeneration("success", time.Millisecond)
	m.RecordPaymentIdempotency(PaymentIdempotencyCreated)
}

// TestObserveHTTPRequest_BoundedLabelsAndHistogram proves the HTTP
// counter carries exactly method/route/status and the duration histogram
// receives a matching observation.
func TestObserveHTTPRequest_BoundedLabelsAndHistogram(t *testing.T) {
	m := New(nil)

	m.ObserveHTTPRequest(http.MethodGet, "/api/v1/invoices/{id}", http.StatusOK, 25*time.Millisecond)

	body := scrape(t, m)
	if !strings.Contains(body, `go_invoicing_http_requests_total{method="GET",route="/api/v1/invoices/{id}",status="200"} 1`) {
		t.Fatalf("expected a bounded-label counter sample, got:\n%s", body)
	}
	if !strings.Contains(body, `go_invoicing_http_request_duration_seconds_bucket{method="GET",route="/api/v1/invoices/{id}"`) {
		t.Fatalf("expected a matching duration histogram bucket sample, got:\n%s", body)
	}
}

// TestRecordWorkerRun_And_RecordWorkerFailure_And_RecordSessionsDeleted
// proves each worker metric records independently and additively.
func TestRecordWorkerRun_And_RecordWorkerFailure_And_RecordSessionsDeleted(t *testing.T) {
	m := New(nil)

	m.RecordWorkerRun(10 * time.Millisecond)
	m.RecordWorkerRun(20 * time.Millisecond)
	m.RecordWorkerFailure()
	m.RecordSessionsDeleted(3)
	m.RecordSessionsDeleted(4)

	body := scrape(t, m)
	if !strings.Contains(body, "go_invoicing_session_cleanup_runs_total 2") {
		t.Fatalf("expected 2 recorded runs, got:\n%s", body)
	}
	if !strings.Contains(body, "go_invoicing_session_cleanup_failures_total 1") {
		t.Fatalf("expected 1 recorded failure, got:\n%s", body)
	}
	if !strings.Contains(body, "go_invoicing_session_cleanup_sessions_deleted_total 7") {
		t.Fatalf("expected 3+4=7 cumulative sessions deleted, got:\n%s", body)
	}
	if !strings.Contains(body, "go_invoicing_session_cleanup_run_duration_seconds_bucket") {
		t.Fatal("expected a run-duration histogram observation")
	}
}

// TestRecordSessionsDeleted_IgnoresZeroAndNegative proves a batch that
// deleted nothing never registers a (meaningless) zero-valued Add call —
// harmless for a counter either way, but asserted explicitly since
// SessionCleanupWorker's own runIteration only calls this when deleted >
// 0 and this method's own nil/zero guard is meant to make that safe to
// call unconditionally too.
func TestRecordSessionsDeleted_IgnoresZeroAndNegative(t *testing.T) {
	m := New(nil)

	m.RecordSessionsDeleted(0)
	m.RecordSessionsDeleted(-1)

	body := scrape(t, m)
	if strings.Contains(body, "go_invoicing_session_cleanup_sessions_deleted_total 1") {
		t.Fatal("expected zero/negative counts not to move the counter")
	}
}

// TestRecordPDFGeneration_ResultLabelIsBoundedEnum proves the PDF
// generation counter uses only the fixed "success"/"error" result
// values, with a matching duration observation, and never anything
// derived from invoice/customer/organisation data (there is nothing
// domain-specific this method's signature could even accept — result is
// a plain string parameter the caller must supply one of two fixed
// values for).
func TestRecordPDFGeneration_ResultLabelIsBoundedEnum(t *testing.T) {
	m := New(nil)

	m.RecordPDFGeneration("success", 5*time.Millisecond)
	m.RecordPDFGeneration("error", 5*time.Millisecond)

	body := scrape(t, m)
	if !strings.Contains(body, `go_invoicing_pdf_generations_total{result="error"} 1`) {
		t.Fatalf("expected one error sample, got:\n%s", body)
	}
	if !strings.Contains(body, `go_invoicing_pdf_generations_total{result="success"} 1`) {
		t.Fatalf("expected one success sample, got:\n%s", body)
	}
	if !strings.Contains(body, "go_invoicing_pdf_generation_duration_seconds_bucket") {
		t.Fatal("expected a PDF generation duration histogram observation")
	}
}

// TestNew_RegistersRuntimeAndDBPoolCollectors proves the standard Go/
// process collectors (section 22) and the DB pool collector (with a nil
// pool, so it emits no samples rather than panicking — section 14/15) are
// both wired in without error.
func TestNew_RegistersRuntimeAndDBPoolCollectors(t *testing.T) {
	m := New(nil)

	body := scrape(t, m)
	if !strings.Contains(body, "go_goroutines ") {
		t.Fatalf("expected the standard Go collector's go_goroutines sample, got:\n%s", body)
	}
	if strings.Contains(body, "go_invoicing_db_pool_") {
		t.Fatal("expected no db_pool samples for a nil pool")
	}
}

// TestNew_ExposesBuildInfoMetric is Milestone 11 Part 2's addition
// (deferred by Milestone 10 Part 4 until build metadata existed): every
// Metrics instance registers a build_info gauge, always 1, labeled with
// whatever internal/buildinfo currently holds — "dev"/"unknown" for this
// ordinary `go test` run, since nothing here was built with -ldflags.
// build_time is deliberately NOT a label — see New's own doc comment on
// buildInfo for why.
func TestNew_ExposesBuildInfoMetric(t *testing.T) {
	m := New(nil)

	body := scrape(t, m)
	want := `go_invoicing_build_info{commit="` + buildinfo.Commit + `",version="` + buildinfo.Version + `"} 1`
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in the exposition, got:\n%s", want, body)
	}
	if strings.Contains(body, "build_time") {
		t.Fatal("expected no build_time label anywhere in the exposition")
	}
}

// TestMetrics_ConcurrentRecordingAndScrapingIsRaceFree is Milestone 10
// Part 5's concurrency audit: it hammers every recording method plus
// concurrent scrapes from many goroutines at once. Meant to be run under
// `go test -race` — Prometheus's own CounterVec/HistogramVec and the
// promhttp handler already document themselves as safe for concurrent
// use; this proves nothing this package adds around them (the
// dbPoolCollector, the small wrapper methods) breaks that, and that no
// application-level lock is needed.
func TestMetrics_ConcurrentRecordingAndScrapingIsRaceFree(t *testing.T) {
	pool := newUnconnectedPool(t)
	m := New(pool)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				m.ObserveHTTPRequest("GET", "/api/v1/invoices/{id}", 200, time.Millisecond)
				m.RecordWorkerRun(time.Millisecond)
				m.RecordWorkerFailure()
				m.RecordSessionsDeleted(1)
				m.RecordPDFGeneration("success", time.Millisecond)
				if id%5 == 0 {
					// A raw ServeHTTP call here, not the scrape() helper:
					// t.Fatalf (which scrape uses on a non-200) must only
					// ever be called from the test's own goroutine, never
					// from one of these spawned ones.
					recorder := httptest.NewRecorder()
					m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestRecordPaymentIdempotency_OutcomeLabelIsBoundedEnum proves the
// Milestone 13 Part 1 counter carries only the three fixed outcomes, and
// that any other value is dropped rather than becoming a new label value.
func TestRecordPaymentIdempotency_OutcomeLabelIsBoundedEnum(t *testing.T) {
	m := New(nil)

	m.RecordPaymentIdempotency(PaymentIdempotencyCreated)
	m.RecordPaymentIdempotency(PaymentIdempotencyReplayed)
	m.RecordPaymentIdempotency(PaymentIdempotencyReplayed)
	m.RecordPaymentIdempotency(PaymentIdempotencyConflict)
	m.RecordPaymentIdempotency("some-raw-idempotency-key-value")

	body := scrape(t, m)
	for _, want := range []string{
		`go_invoicing_payment_idempotency_total{outcome="created"} 1`,
		`go_invoicing_payment_idempotency_total{outcome="replayed"} 2`,
		`go_invoicing_payment_idempotency_total{outcome="conflict"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "some-raw-idempotency-key-value") {
		t.Error("expected an unrecognised outcome never to become a label value")
	}
}
