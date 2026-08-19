// Command feedla is the single-binary feed reader server and CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/config"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/maintenance"
	"github.com/tokuhirom/feedla/internal/metrics"
	"github.com/tokuhirom/feedla/internal/remotebackup"
	"github.com/tokuhirom/feedla/internal/store"
	"github.com/tokuhirom/feedla/internal/web"
)

// version is injected via -ldflags "-X main.version=..." by .goreleaser.yaml;
// binaries built without it (e.g. `go build`, the plain Dockerfile) report
// "unknown".
var version = "unknown"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "import-opml":
		err = cmdImportOPML(os.Args[2:])
	case "crawl":
		err = cmdCrawl(os.Args[2:])
	case "backup":
		err = cmdBackup(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "feedla:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: feedla <command> [args]

commands:
  import-opml <file.opml>   import feeds/folders from an OPML file
  crawl [--due] [--limit N] fetch/parse/write feeds once (default: every known feed)
  backup <dest.db>          on-demand consistent snapshot of the live DB (VACUUM INTO; safe to run while feedla serve is running)
  serve [--tick D] [--batch N] [--listen ADDR]  run the HTTP API and crawler scheduler until interrupted`)
}

func cmdImportOPML(args []string) error {
	fs := flag.NewFlagSet("import-opml", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError already exits on parse failure
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: feedla import-opml <file.opml>")
	}
	path := fs.Arg(0)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", cfg.DBPath, err)
	}
	defer st.Close()

	// This CLI is an operator tool run directly against the DB file, not
	// through the multi-user HTTP API -- it always targets the bootstrap
	// admin (id=1, unconditionally seeded by migration 0005).
	const bootstrapAdminID = 1
	n, err := feed.ImportOPML(context.Background(), st, bootstrapAdminID, f, 0)
	if err != nil {
		return err
	}

	fmt.Printf("imported %d feed(s) into %s\n", n, cfg.DBPath)
	return nil
}

func cmdCrawl(args []string) error {
	fs := flag.NewFlagSet("crawl", flag.ExitOnError)
	due := fs.Bool("due", false, "only crawl feeds whose next_fetch_at has passed, instead of every known feed")
	limit := fs.Int("limit", 200, "max feeds to crawl when --due is set")
	_ = fs.Parse(args) // flag.ExitOnError already exits on parse failure

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", cfg.DBPath, err)
	}
	defer st.Close()

	fetcher := crawler.NewFetcher(crawler.FetcherConfig{UserAgent: cfg.UserAgent})
	cr := crawler.New(st, fetcher, cfg.FetchConcurrency, cfg.FetchMinInterval, cfg.FetchMaxInterval)

	now := time.Now()
	ctx := context.Background()
	var summary *crawler.Summary
	if *due {
		summary, err = cr.CrawlDue(ctx, now, *limit)
	} else {
		summary, err = cr.CrawlAll(ctx, now)
	}
	if err != nil {
		return err
	}

	for _, r := range summary.Results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "feedla: crawl %s: %v\n", r.FeedURL, r.Err)
			continue
		}
		if r.NewEntries > 0 {
			fmt.Printf("%s: %d new entr%s\n", r.FeedURL, r.NewEntries, plural(r.NewEntries))
		}
	}
	fmt.Printf("crawled %d feed(s): %d new entr%s, %d error(s)\n",
		summary.Feeds, summary.NewEntries, plural(summary.NewEntries), summary.Errors)
	return nil
}

// cmdBackup writes a consistent snapshot of the live DB to dest via
// store.BackupTo's VACUUM INTO (safe under WAL and concurrent use, atomic
// tmp-file-then-rename). Unlike internal/maintenance's daily backup (which
// only runs from inside a running `feedla serve` process, gated by
// FR_BACKUP_DIR), this is an on-demand operator command -- e.g. to snapshot
// before a schema-changing deploy -- runnable as a separate process against
// the same DB file a `feedla serve` process may already have open (SQLite's
// WAL mode makes that safe).
func cmdBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError already exits on parse failure
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: feedla backup <dest.db>")
	}
	dest := fs.Arg(0)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", cfg.DBPath, err)
	}
	defer st.Close()

	if err := st.BackupTo(context.Background(), dest); err != nil {
		return err
	}

	fmt.Printf("backed up %s to %s\n", cfg.DBPath, dest)
	return nil
}

func cmdServe(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	tick := fs.Duration("tick", 30*time.Second, "scheduler poll interval")
	batch := fs.Int("batch", 200, "max feeds claimed per tick")
	listen := fs.String("listen", cfg.Listen, "address to listen on (overrides FR_LISTEN)")
	_ = fs.Parse(args) // flag.ExitOnError already exits on parse failure
	cfg.Listen = *listen

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", cfg.DBPath, err)
	}
	defer st.Close()

	if n, err := feed.SeedIfEmpty(context.Background(), st); err != nil {
		return fmt.Errorf("seed default subscriptions: %w", err)
	} else if n > 0 {
		slog.Info("feedla: seeded default subscriptions", "count", n)
	}

	hostSem := crawler.NewHostSemaphore(0, time.Second)
	fetcher := crawler.NewFetcher(crawler.FetcherConfig{UserAgent: cfg.UserAgent, HostSem: hostSem})
	cr := crawler.New(st, fetcher, cfg.FetchConcurrency, cfg.FetchMinInterval, cfg.FetchMaxInterval)
	m := metrics.New()
	cr.SetMetrics(m)
	sched := crawler.NewScheduler(cr, hostSem, *tick, *batch)
	var remote maintenance.RemoteUploader
	if cfg.BackupRemote.Endpoint != "" {
		remote = remotebackup.New(remotebackup.Config{
			Endpoint:    cfg.BackupRemote.Endpoint,
			Region:      cfg.BackupRemote.Region,
			Bucket:      cfg.BackupRemote.Bucket,
			AccessKey:   cfg.BackupRemote.AccessKey,
			SecretKey:   cfg.BackupRemote.SecretKey,
			Prefix:      cfg.BackupRemote.Prefix,
			Generations: cfg.BackupRemote.Generations,
		})
		slog.Info("feedla: remote backup enabled",
			"endpoint", cfg.BackupRemote.Endpoint, "bucket", cfg.BackupRemote.Bucket, "generations", cfg.BackupRemote.Generations)
	}
	maint := maintenance.NewRunner(st, maintenance.Config{
		RetentionDays:    cfg.RetentionDays,
		RetentionPerFeed: cfg.RetentionPerFeed,
		BackupDir:        cfg.BackupDir,
		Remote:           remote,
	})

	spaHandler, err := web.Handler()
	if err != nil {
		return fmt.Errorf("build web handler: %w", err)
	}
	mux := http.NewServeMux()
	apiHandler := api.NewHandler(st, cr, fetcher, m, api.Options{
		CookieSecure: cfg.CookieSecure,
		PublicOrigin: cfg.PublicOrigin,
		MetricsToken: cfg.MetricsToken,
		Quota:        cfg.Quota,
		Version:      version,
	})
	mux.Handle("/api/", apiHandler)
	mux.Handle("/healthz", apiHandler)
	mux.Handle("/metrics", apiHandler)
	mux.Handle("/", spaHandler)

	// ReadHeaderTimeout guards against slowloris-style connections that
	// trickle headers in forever; there's no similarly clear-cut default
	// for ReadTimeout/WriteTimeout since OPML export/import can legitimately
	// take a while on a large subscription list.
	httpSrv := &http.Server{Addr: cfg.Listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// errCh takes exactly one report per goroutine below, so the final
	// drain always receives exactly three values.
	errCh := make(chan error, 3)
	go func() {
		slog.Info("feedla: http server starting", "addr", cfg.Listen)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
			return
		}
		errCh <- nil
	}()
	go func() {
		slog.Info("feedla: scheduler starting", "tick", *tick, "batch", *batch, "db", cfg.DBPath)
		if err := sched.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("scheduler: %w", err)
			return
		}
		errCh <- nil
	}()
	go func() {
		slog.Info("feedla: maintenance starting",
			"retention_days", cfg.RetentionDays, "retention_per_feed", cfg.RetentionPerFeed, "backup_dir", cfg.BackupDir)
		if err := maint.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("maintenance: %w", err)
			return
		}
		errCh <- nil
	}()

	<-ctx.Done()
	slog.Info("feedla: shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("feedla: http server shutdown", "error", err)
	}

	err1, err2, err3 := <-errCh, <-errCh, <-errCh
	if err := errors.Join(err1, err2, err3); err != nil {
		return err
	}
	slog.Info("feedla: stopped")
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
