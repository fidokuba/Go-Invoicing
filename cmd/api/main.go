package main

import (
	"context"
	"go-invoicing/internal/app"
	"go-invoicing/internal/config"
	"go-invoicing/internal/database"
	"log"
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
	config := config.Load()
	logger.Info("configuration loaded", "APP_ENV", config.Environment, "DATABASE_URL", config.DatabaseURL, "APP_PORT", config.Port)

	// Create the database pool
	database, err := database.NewPostgresPool(ctx, config.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	// Create the app
	app := app.New(database)
	_ = app

	// Create an HTTP server
	server := &http.Server{
		Addr:    ":" + config.Port,
		Handler: app.Handler(),
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
