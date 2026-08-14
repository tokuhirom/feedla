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
	"syscall"
	"time"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/config"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
	"github.com/tokuhirom/feedla/internal/web"
)

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

	hostSem := crawler.NewHostSemaphore(0, time.Second)
	fetcher := crawler.NewFetcher(crawler.FetcherConfig{
		UserAgent: cfg.UserAgent,
		HostSem:   hostSem,
		// Deliberately unrestricted: see the package doc comment above.
		DialContext: (&net.Dialer{}).DialContext,
	})
	cr := crawler.New(st, fetcher, cfg.FetchConcurrency, cfg.FetchMinInterval, cfg.FetchMaxInterval)
	sched := crawler.NewScheduler(cr, hostSem, time.Hour, 200)

	spaHandler, err := web.Handler()
	if err != nil {
		return fmt.Errorf("build web handler: %w", err)
	}
	mux := http.NewServeMux()
	apiHandler := api.NewHandler(st, cr, fetcher)
	mux.Handle("/api/", apiHandler)
	mux.Handle("/healthz", apiHandler)
	mux.Handle("/", spaHandler)

	httpSrv := &http.Server{Addr: cfg.Listen, Handler: mux}

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
