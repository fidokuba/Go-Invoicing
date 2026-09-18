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
	application := app.New(db)

	// Create an HTTP server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: application.Handler(),
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
