package crawler_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

const testFeedTemplate = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>%s</title>
<link>https://example.com/</link>
<item>
  <title>%s</title>
  <link>https://example.com/1</link>
  <guid>guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body 1</description>
</item>
<item>
  <title>Item 2</title>
  <link>https://example.com/2</link>
  <guid>guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body 2</description>
</item>
</channel></rss>`

// newTestFetcher returns a Fetcher whose dialer isn't SSRF-restricted, so it
// can reach httptest.Server on 127.0.0.1.
func newTestFetcher() *crawler.Fetcher {
	return crawler.NewFetcher(crawler.FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
	})
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCrawlerFetchesParsesAndWrites(t *testing.T) {
	const etag = `"v1"`
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, testFeedTemplate, "Test Feed", "Item 1")
	}))
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, srv.URL+"/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}

	cr := crawler.New(st, newTestFetcher(), 4)

	// First crawl: 200 with two entries.
	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}
	if summary.Errors != 0 {
		t.Fatalf("summary.Errors = %d, want 0 (results: %+v)", summary.Errors, summary.Results)
	}
	if summary.NewEntries != 2 {
		t.Fatalf("summary.NewEntries = %d, want 2", summary.NewEntries)
	}

	count, err := st.CountEntries(ctx, feedID)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountEntries = %d, want 2", count)
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	f := feeds[0]
	if f.Title != "Test Feed" {
		t.Errorf("feed title = %q, want %q", f.Title, "Test Feed")
	}
	if f.ETag == nil || *f.ETag != etag {
		t.Errorf("feed etag = %v, want %q", f.ETag, etag)
	}
	if f.LastStatus == nil || *f.LastStatus != http.StatusOK {
		t.Errorf("feed last_status = %v, want 200", f.LastStatus)
	}
	if f.ErrorCount != 0 {
		t.Errorf("feed error_count = %d, want 0", f.ErrorCount)
	}

	// Second crawl: server returns 304 because our ETag matches; no new
	// entries, no error, and the row count doesn't change.
	now2 := now.Add(time.Hour)
	summary, err = cr.CrawlAll(ctx, now2)
	if err != nil {
		t.Fatalf("second CrawlAll: %v", err)
	}
	if summary.Errors != 0 || summary.NewEntries != 0 {
		t.Fatalf("second crawl summary = %+v, want no errors/new entries", summary)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (one per crawl)", requests)
	}
	count, err = st.CountEntries(ctx, feedID)
	if err != nil {
		t.Fatalf("CountEntries after second crawl: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountEntries after second crawl = %d, want still 2", count)
	}
}

func TestCrawlerPreservesReadState(t *testing.T) {
	title := "Original Title"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, testFeedTemplate, "Test Feed", title)
	}))
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, srv.URL+"/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	cr := crawler.New(st, newTestFetcher(), 4)

	if _, err := cr.CrawlAll(ctx, now); err != nil {
		t.Fatalf("first CrawlAll: %v", err)
	}

	// Mark guid-1's entry as read, as if the user had read it.
	if _, err := st.Write.ExecContext(ctx, `UPDATE entries SET read_at = ? WHERE feed_id = ? AND guid = 'guid-1'`, now.Unix(), feedID); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	// The upstream feed changes the title of guid-1's entry (server has no
	// ETag, so it always returns 200 with fresh content).
	title = "Updated Title"
	now2 := now.Add(time.Hour)
	if _, err := cr.CrawlAll(ctx, now2); err != nil {
		t.Fatalf("second CrawlAll: %v", err)
	}

	var gotTitle string
	var readAt *int64
	err = st.Read.QueryRowContext(ctx, `SELECT title, read_at FROM entries WHERE feed_id = ? AND guid = 'guid-1'`, feedID).Scan(&gotTitle, &readAt)
	if err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if gotTitle != "Updated Title" {
		t.Errorf("title = %q, want %q (re-fetch should update body/title)", gotTitle, "Updated Title")
	}
	if readAt == nil {
		t.Error("read_at was cleared by re-fetch; it must stay set once read")
	}
}
