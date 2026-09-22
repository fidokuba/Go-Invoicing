package main

import (
	"context"
	"errors"
	"fmt"
	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/app"
	"go-invoicing/internal/buildinfo"
	"go-invoicing/internal/config"
	"go-invoicing/internal/database"
	"go-invoicing/internal/metrics"
	"io"
	"log/slog"
	"net"
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

// versionRequested reports whether args (main passes os.Args[1:]) asked
// for build-metadata output. Checked before anything else in main so
// `--version`/`-version` needs no configuration, opens no database
// connection, runs no migration, and starts no worker or server — it
// only ever prints buildinfo.String() and returns. A hand-written check
// over a single flag is deliberately used instead of the stdlib `flag`
// package (which would still work, but adds its own -h/--help handling
// and global flag.CommandLine state this one flag doesn't need) or a
// third-party CLI framework, per this milestone's own "keep argument
// handling minimal" instruction.
func versionRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--version" || arg == "-version" {
			return true
		}
	}

	return false
}

// newLogger builds this process's one application logger — the
// composition root's single point of *slog.Logger construction (see
// main; no other package ever calls slog.New). format and level are
// assumed already validated by config.Load (see config.getEnumEnv), so
// only "json" is checked explicitly below — anything else (i.e. "text",
// the only other value config.Load ever allows through) uses the text
// handler.
func newLogger(w io.Writer, format, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLogLevel(level)}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	return slog.New(handler)
}

// parseLogLevel maps config's validated LOG_LEVEL string onto the
// matching slog.Level. Like newLogger above, level is assumed already
// validated by config.Load — the default case exists only to give
// "info" (and, defensively, any value config.Load's own validation
// didn't catch) a safe level rather than requiring an error return here
// too.
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
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
	// --version prints build metadata and exits immediately — before
	// configuration, logging, the database, or anything else this
	// process would otherwise initialize. See versionRequested's own doc
	// comment.
	if versionRequested(os.Args[1:]) {
		fmt.Println(buildinfo.String())
		return
	}

	// bootstrapLogger exists only to report a failure to load
	// configuration itself (immediately below): the real logger's
	// format/level are themselves config values (LOG_FORMAT/LOG_LEVEL),
	// so they can't be known yet if config.Load has just failed. Once
	// config loads successfully, logger (built from cfg) is the only
	// logger the rest of this process ever uses — this is still "one
	// application logger," not a logging abstraction: the unavoidable
	// chicken-and-egg case of a config load failure is the sole reason a
	// second slog.New call exists in this file at all.
	bootstrapLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))

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
		bootstrapLogger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	logger := newLogger(os.Stdout, cfg.LogFormat, cfg.LogLevel)

	// A single startup build-metadata event (Milestone 11 Part 2, closing
	// the gap Milestone 10 Part 4 deliberately deferred) — never
	// re-attached to every subsequent log line; version/commit/build_time
	// are otherwise available via the go_invoicing_build_info metric
	// (when METRICS_ENABLED) and `--version`, both reading the same
	// internal/buildinfo values.
	logger.Info(
		"go-invoicing starting",
		"version", buildinfo.Version,
		"commit", buildinfo.Commit,
		"build_time", buildinfo.BuildTime,
	)

	logger.Info(
		"configuration loaded",
		"APP_ENV", cfg.Environment,
		"APP_HOST", cfg.Host,
		"APP_PORT", cfg.Port,
		"LOG_FORMAT", cfg.LogFormat,
		"LOG_LEVEL", cfg.LogLevel,
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

	// Create an HTTP server. net.JoinHostPort (not naive "host:port"
	// string concatenation) correctly brackets an IPv6 Host — e.g.
	// APP_HOST=::1 becomes "[::1]:8080" rather than the malformed
	// "::1:8080" a plain cfg.Host+":"+cfg.Port would produce. cfg.Host
	// defaults to "" (every interface), and JoinHostPort("", "8080")
	// yields ":8080" — byte-for-byte the address this server bound
	// before APP_HOST existed, so an operator who never sets it sees no
	// behaviour change at all.
	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
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
