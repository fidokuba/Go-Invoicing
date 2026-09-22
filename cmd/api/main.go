package main

import (
	"context"
	"errors"
	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/app"
	"go-invoicing/internal/config"
	"go-invoicing/internal/database"
	"go-invoicing/internal/metrics"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Server timeout defaults (Milestone 8 Part 2). The API only ever
// exchanges small JSON payloads and one synchronous binary response (PDF
// generation, which the Milestone 7 hardening pass measured completing
// in milliseconds even for a 500-line invoice) — there is no
// long-running or streaming endpoint these need to accommodate, so
// conservative, fixed values are used rather than building configuration
// for a knob nothing yet needs to turn.
const (
	// serverReadHeaderTimeout bounds how long a client may take to send
	// request headers — generous for an ordinary browser/API client, but
	// short enough to make a slow-header (Slowloris-style) connection
	// give up quickly rather than tying up a server goroutine.
	serverReadHeaderTimeout = 5 * time.Second

	// serverReadTimeout bounds the entire request (headers and body).
	// The largest legitimate body is a JSON invoice with many lines,
	// bounded itself by httpx.MaxRequestBodyBytes (2 MiB) — this is
	// comfortably longer than reading that could ever take on any real
	// connection.
	serverReadTimeout = 10 * time.Second

	// serverWriteTimeout bounds how long writing the response may take,
	// measured from the end of the request header read. It must
	// comfortably exceed PDF generation's own worst case; 500 lines
	// renders in well under a second, so this leaves wide headroom.
	serverWriteTimeout = 15 * time.Second

	// serverIdleTimeout bounds how long a keep-alive connection may sit
	// idle between requests before the server closes it, freeing the
	// file descriptor for a genuinely active client.
	serverIdleTimeout = 60 * time.Second
)

type lifecycleServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

func runServerLifecycle(
	ctx context.Context,
	stop context.CancelFunc,
	logger *slog.Logger,
	server lifecycleServer,
	shutdownTimeout time.Duration,
	workerWait func(),
) error {
	serverErrCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		select {
		case serverErrCh <- err:
		default:
		}
	}()

	var unexpectedErr error
	select {
	case <-ctx.Done():
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			unexpectedErr = err
			logger.Error("unexpected HTTP server termination", "error", err)
			stop()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server shutdown failed", "error", err)
		if unexpectedErr == nil {
			unexpectedErr = err
		}
	}

	if workerWait != nil {
		workerWait()
	}

	return unexpectedErr
}

func main() {

	logger := slog.New(
		slog.NewTextHandler(os.Stdout, nil),
	)

	// The one root, process-level context: cancelled on SIGINT/SIGTERM.
	// Everything that needs to react to shutdown — pool startup, the
	// background worker, and the final wait below — shares this single
	// context rather than each holding its own unrelated
	// context.Background().
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logger.Info(
		"configuration loaded",
		"APP_ENV", cfg.Environment,
		"APP_PORT", cfg.Port,
	)

	// Run database migrations before creating the pool.
	if err := database.Migrate(cfg.DatabaseURL); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}

	// Create the database pool
	db, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error(
			"failed to connect to database",
			"error", err,
		)
		os.Exit(1)
	}
	defer db.Close()

	// Metrics (Milestone 10 Part 4): m stays a typed nil when
	// METRICS_ENABLED=false, which is enough on its own to disable
	// everything — GET /metrics is never mounted (see App.Handler), and
	// every *metrics.Metrics method used across the application
	// (HTTP/worker/PDF recording, the DB pool collector) is a nil-safe
	// no-op. Constructed with the real pool so its DB-pool collector can
	// report live pgxpool.Stat() gauges at scrape time.
	var m *metrics.Metrics
	if cfg.MetricsEnabled {
		m = metrics.New(db)
	}

	// Create the app
	application := app.New(db, logger, m)

	// Create an HTTP server
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           application.Handler(),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}

	// Start the session cleanup background worker (Milestone 6). It shares
	// the same root ctx as the HTTP server's shutdown trigger, and the
	// same database pool — no separate process, no separate connections.
	var workerWg sync.WaitGroup
	if cfg.WorkerEnabled {
		sessionRepository := admin.NewPostgresSessionRepository(db)
		worker := admin.NewSessionCleanupWorker(sessionRepository, cfg.WorkerInterval, cfg.WorkerBatchSize, logger, m)

		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			worker.Run(ctx)
		}()
	}

	logger.Info("API server listening", "addr", server.Addr)
	err = runServerLifecycle(ctx, stop, logger, server, 5*time.Second, workerWg.Wait)
	if err != nil {
		logger.Error("runtime shutdown failed", "error", err)
		return
	}

	logger.Info("database pool closed")
}
