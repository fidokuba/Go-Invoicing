package main

import (
	"context"
	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/app"
	"go-invoicing/internal/config"
	"go-invoicing/internal/database"
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

	// Create the app
	application := app.New(db, logger)

	// Create an HTTP server
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           application.Handler(),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}

	// Start the server
	go func() {
		logger.Info("API server listening", "addr", server.Addr)

		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
		}
	}()

	// Start the session cleanup background worker (Milestone 6). It shares
	// the same root ctx as the HTTP server's shutdown trigger, and the
	// same database pool — no separate process, no separate connections.
	var workerWg sync.WaitGroup
	if cfg.WorkerEnabled {
		sessionRepository := admin.NewPostgresSessionRepository(db)
		worker := admin.NewSessionCleanupWorker(sessionRepository, cfg.WorkerInterval, cfg.WorkerBatchSize, logger)

		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			worker.Run(ctx)
		}()
	}

	// Handle shutdown & close the database pool
	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Info("shutting down HTTP server")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", "error", err)
	}

	// The worker already stopped taking new batches when ctx was
	// cancelled above; wait for its goroutine to actually finish before
	// the deferred db.Close() runs, so the pool can never close out from
	// under an in-flight cleanup query.
	workerWg.Wait()

	logger.Info("database pool closed")
}
