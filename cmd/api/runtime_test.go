package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

type stubServer struct {
	listenErr      error
	shutdownErr    error
	shutdownCalled bool
}

func (s *stubServer) ListenAndServe() error {
	return s.listenErr
}

func (s *stubServer) Shutdown(ctx context.Context) error {
	s.shutdownCalled = true
	return s.shutdownErr
}

func TestRunServerLifecycle_UnexpectedServerFailureCancelsAndReturnsError(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	server := &stubServer{listenErr: errors.New("listen failed")}
	workerDone := make(chan struct{})

	err := runServerLifecycle(ctx, stop, slog.New(slog.NewTextHandler(io.Discard, nil)), server, 50*time.Millisecond, func() {
		<-ctx.Done()
		close(workerDone)
	})
	if !errors.Is(err, server.listenErr) {
		t.Fatalf("expected listen error %v, got %v", server.listenErr, err)
	}
	if ctx.Err() == nil {
		t.Fatal("expected root context to be cancelled after unexpected server failure")
	}
	if !server.shutdownCalled {
		t.Fatal("expected shutdown to be invoked after unexpected server failure")
	}
	<-workerDone
}

func TestRunServerLifecycle_ExpectedErrServerClosedIsIgnored(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	server := &stubServer{listenErr: http.ErrServerClosed}

	err := runServerLifecycle(ctx, stop, slog.New(slog.NewTextHandler(io.Discard, nil)), server, 50*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("expected http.ErrServerClosed to be treated as expected, got %v", err)
	}
	if !server.shutdownCalled {
		t.Fatal("expected shutdown to still be invoked during normal lifecycle cleanup")
	}
}

func TestRunServerLifecycle_ShutdownTimeoutLeavesBlockedHandlerRunning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	start := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(start)
		<-release
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	<-start

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Fatalf("expected shutdown deadline exceeded, got %v", shutdownErr)
	}

	select {
	case err := <-errCh:
		t.Fatalf("request should still be active after shutdown timeout, client error: %v", err)
	case <-respCh:
		t.Fatal("request completed before the shutdown timeout was allowed to expire")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case resp := <-respCh:
		if resp == nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}
	case err := <-errCh:
		if err != nil {
			t.Fatalf("request failed after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish after release")
	}
}

func TestRunServerLifecycle_GracefulShutdownAllowsRequestToFinish(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	started := make(chan struct{})
	finish := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-finish
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		_ = server.Serve(listener)
	}()

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	go func() {
		_ = server.Shutdown(shutdownCtx)
		close(finish)
	}()
	cancel()

	select {
	case resp := <-respCh:
		if resp == nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected success, got response=%v", resp)
		}
	case err := <-errCh:
		t.Fatalf("request failed during graceful shutdown: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("request did not finish before graceful shutdown deadline")
	}
}
