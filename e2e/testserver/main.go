// Command testserver runs feedla's real HTTP API + SPA + crawler wiring
// (the same pieces `feedla serve` uses) but with the crawler's SSRF-blocking
// dialer swapped for a plain net.Dialer.
//
// This exists ONLY so e2e/tests/*.spec.ts can subscribe to a fixture feed
// server bound to 127.0.0.1 -- the production dialer in
// internal/crawler/dialer.go correctly refuses to fetch loopback/private
// addresses, which is exactly what a local fixture server looks like. This
// binary is never built by `make build` and must never be deployed; it only
// exists for `make e2e` / playwright.config.ts's webServer.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/auth"
	"github.com/tokuhirom/feedla/internal/config"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/metrics"
	"github.com/tokuhirom/feedla/internal/store"
	"github.com/tokuhirom/feedla/internal/web"
)

// E2E-only fixed admin credentials, seeded below so e2e/tests/auth.setup.ts
// can log in without going through the interactive setup screen. Never
// used outside this test-only binary (see the package doc comment).
const (
	e2eUsername = "e2e-admin"
	e2ePassword = "e2e-test-password-12345"
)

// delayMiddleware optionally holds POST /api/v1/entries/read open for
// FEEDLA_E2E_DELAY_MARK_READ_MS before handling it, so a test can reproduce
// races around that request still being in flight when the page navigates
// away (e.g. a reload racing the debounced mark-read POST). This is a real
// server-side delay rather than a Playwright route() pause: route()
// interception ties the request to the page's frame and cancels it on
// navigation regardless of fetch keepalive, so it can't be used to test
// keepalive semantics faithfully.
func delayMiddleware(next http.Handler) http.Handler {
	delay, _ := strconv.Atoi(os.Getenv("FEEDLA_E2E_DELAY_MARK_READ_MS"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 && r.Method == http.MethodPost && r.URL.Path == "/api/v1/entries/read" {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e testserver:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", cfg.DBPath, err)
	}
	defer st.Close()

	if err := seedE2EAdmin(context.Background(), st); err != nil {
		return fmt.Errorf("seed e2e admin: %w", err)
	}

	hostSem := crawler.NewHostSemaphore(0, time.Second)
	fetcher := crawler.NewFetcher(crawler.FetcherConfig{
		UserAgent: cfg.UserAgent,
		HostSem:   hostSem,
		// Deliberately unrestricted: see the package doc comment above.
		DialContext: (&net.Dialer{}).DialContext,
	})
	cr := crawler.New(st, fetcher, cfg.FetchConcurrency, cfg.FetchMinInterval, cfg.FetchMaxInterval)
	m := metrics.New()
	cr.SetMetrics(m)
	sched := crawler.NewScheduler(cr, hostSem, time.Hour, 200)

	spaHandler, err := web.Handler()
	if err != nil {
		return fmt.Errorf("build web handler: %w", err)
	}
	mux := http.NewServeMux()
	apiHandler := api.NewHandler(st, cr, fetcher, m, api.Options{})
	mux.Handle("/api/", apiHandler)
	mux.Handle("/healthz", apiHandler)
	mux.Handle("/metrics", apiHandler)
	mux.Handle("/", spaHandler)

	httpSrv := &http.Server{Addr: cfg.Listen, Handler: delayMiddleware(mux)}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 2)
	go func() {
		slog.Info("e2e testserver: http server starting", "addr", cfg.Listen)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
			return
		}
		errCh <- nil
	}()
	go func() {
		if err := sched.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("scheduler: %w", err)
			return
		}
		errCh <- nil
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("e2e testserver: http server shutdown", "error", err)
	}

	err1, err2 := <-errCh, <-errCh
	return errors.Join(err1, err2)
}

// seedE2EAdmin completes the bootstrap admin's setup with a fixed
// username/password so e2e/tests/auth.setup.ts can log in directly,
// without driving the interactive setup screen through the browser. A
// no-op if setup was already completed (e.g. a leftover DB from a
// previous run with the same --db path).
func seedE2EAdmin(ctx context.Context, st *store.Store) error {
	pending, err := st.IsSetupPending(ctx, 1)
	if err != nil {
		return err
	}
	if !pending {
		return nil
	}
	hash, err := auth.HashPassword(e2ePassword)
	if err != nil {
		return err
	}
	return st.CompleteSetup(ctx, 1, e2eUsername, hash, time.Now())
}
