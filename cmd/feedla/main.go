// Command feedla is the single-binary feed reader server and CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/tokuhirom/feedla/internal/config"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

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
  crawl [--due] [--limit N] fetch/parse/write feeds (default: every known feed)`)
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

	n, err := feed.ImportOPML(context.Background(), st, f)
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
	cr := crawler.New(st, fetcher, cfg.FetchConcurrency)

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

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
