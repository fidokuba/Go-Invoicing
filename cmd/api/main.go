package main

import (
	"context"
	"go-invoicing/internal/app"
	"go-invoicing/internal/config"
	"go-invoicing/internal/database"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {

	logger := slog.New(
		slog.NewTextHandler(os.Stdout, nil),
	)

	// 	Create a context
	ctx := context.Background()

	// Load configuration
	cfg := config.Load()
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

	// Handle shutdown & close the database pool
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Info("shutting down HTTP server")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", "error", err)
	}

	logger.Info("database pool closed")
}
