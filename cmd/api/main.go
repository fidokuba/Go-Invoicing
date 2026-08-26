package main

import (
	"context"
	"fmt"
	"go-invoicing/internal/app"
	"go-invoicing/internal/config"
	"go-invoicing/internal/database"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// 	Create a context
	ctx := context.Background()

	// Load configuration
	config := config.Load()

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
		fmt.Printf("API server listening on %s", server.Addr)

		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			log.Printf("server failed: %v", err)
		}
	}()

	// Handle shutdown & close the database pool
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}
}
