package crawler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

// newScrapeSubscription sets up a pagewatch-watched feed row + scrape_sources
// row for srv.URL, mirroring what POST /api/v1/scrape_sources will do once
// the API layer exists (step #5) -- CreateScrapeSource itself is exercised
// directly here since that endpoint isn't wired up yet.
func newScrapeSubscription(t *testing.T, st *store.Store, srvURL, config string, now time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	feedID, err := st.UpsertFeed(ctx, crawler.ScrapePrefix+srvURL, "", "", 3600, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	var cfg []byte
	if config != "" {
		cfg = []byte(config)
	}
	if _, err := st.CreateScrapeSource(ctx, testUserID, feedID, "pagewatch", srvURL, cfg, now); err != nil {
		t.Fatalf("CreateScrapeSource: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	return feedID
}

func TestCrawlerScrapeSourceInitialAndDiff(t *testing.T) {
	var page atomic.Value
	page.Store(`<html><body><p>1件目の記事です。</p></body></html>`)
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, page.Load().(string))
	}))
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	feedID := newScrapeSubscription(t, st, srv.URL, "", now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	// First crawl: no prior state, so pagewatch emits one "monitoring
	// started" entry (§6.6 of the pagewatch design).
	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll (initial): %v", err)
	}
	if summary.Errors != 0 || summary.NewEntries != 1 {
		t.Fatalf("initial crawl summary = %+v, want 0 errors / 1 new entry", summary)
	}

	// Second crawl: the page changed, so this is a real diff -> one more entry.
	page.Store(`<html><body><p>1件目の記事です。</p><p>2件目の新しい記事です。</p></body></html>`)
	summary, err = cr.CrawlAll(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll (changed): %v", err)
	}
	if summary.Errors != 0 || summary.NewEntries != 1 {
		t.Fatalf("changed crawl summary = %+v, want 0 errors / 1 new entry", summary)
	}

	count, err := st.CountEntries(ctx, feedID)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountEntries = %d, want 2 (initial + one diff)", count)
	}

	// The diff entry went through the same bodyPolicy.Sanitize as a real
	// feed's body (ParsedFeed -> normalizeItem, unchanged for pagewatch) --
	// confirm <ins> survives sanitization and carries the added text.
	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	diffEntry := entries[0] // newest first
	if !strings.Contains(diffEntry.Body, "<ins>") || !strings.Contains(diffEntry.Body, "2件目の新しい記事です") {
		t.Errorf("diff entry body = %q, want sanitized <ins> with the added text", diffEntry.Body)
	}

	// Third crawl: identical page -> no new entry, and no wasted state write.
	summary, err = cr.CrawlAll(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll (unchanged): %v", err)
	}
	if summary.Errors != 0 || summary.NewEntries != 0 {
		t.Fatalf("unchanged crawl summary = %+v, want 0 errors / 0 new entries", summary)
	}
	count, err = st.CountEntries(ctx, feedID)
	if err != nil {
		t.Fatalf("CountEntries after unchanged crawl: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountEntries after unchanged crawl = %d, want still 2", count)
	}

	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("requests = %d, want 3 (one per crawl)", got)
	}
}

func TestCrawlerScrapeSourceIgnorePatternsSuppressNoise(t *testing.T) {
	page := func(ts string) string {
		return `<html><body><p>本文は変わりません。</p><p>Document ID: ` + ts + `</p></body></html>`
	}
	var current atomic.Value
	current.Store(page("aaa111"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, current.Load().(string))
	}))
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	cfg := `{"ignore_patterns":["Document ID: [A-Za-z0-9]+"]}`
	feedID := newScrapeSubscription(t, st, srv.URL, cfg, now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	if _, err := cr.CrawlAll(ctx, now); err != nil {
		t.Fatalf("CrawlAll (initial): %v", err)
	}
	if _, err := cr.CrawlAll(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("CrawlAll (establish baseline): %v", err)
	}

	// Only the ignored "Document ID" text changes -- must not produce a new entry.
	current.Store(page("bbb222"))
	summary, err := cr.CrawlAll(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll (noise only): %v", err)
	}
	if summary.NewEntries != 0 {
		t.Fatalf("summary.NewEntries = %d, want 0: only the ignore_patterns-masked text changed", summary.NewEntries)
	}

	count, err := st.CountEntries(ctx, feedID)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountEntries = %d, want 1 (only the initial entry)", count)
	}
}

func TestCrawlerScrapeSourcePermanentRedirectKeepsPrefix(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><body><p>移転後のページです。</p></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	feedID := newScrapeSubscription(t, st, srv.URL+"/old", "", now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	if _, err := cr.CrawlAll(ctx, now); err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}

	f, err := st.GetFeed(ctx, feedID)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	want := crawler.ScrapePrefix + srv.URL + "/new"
	if f.FeedURL != want {
		t.Fatalf("FeedURL = %q, want %q (pseudo-scheme must survive a permanent redirect)", f.FeedURL, want)
	}

	// A second crawl must still be routed as a scrape source, not parsed as
	// an XML feed -- if the ScrapePrefix had been lost, this would fail.
	if _, err := cr.CrawlAll(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("second CrawlAll: %v", err)
	}
	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if feeds[0].ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0", feeds[0].ErrorCount)
	}
}
