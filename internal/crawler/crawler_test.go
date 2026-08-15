package crawler_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
		// Real HostSemaphore politeness gaps would needlessly slow the test
		// suite down; keep the concurrency cap but drop the 1s gap.
		HostSem: crawler.NewHostSemaphore(2, 0),
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

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

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

type fakeRecorder struct {
	observations []string
	durations    []time.Duration
}

func (r *fakeRecorder) ObserveFetch(status string, d time.Duration) {
	r.observations = append(r.observations, status)
	r.durations = append(r.durations, d)
}

func TestCrawlerReportsStatusAndMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, testFeedTemplate, "Test Feed", "Item 1")
	}))
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()

	if _, err := st.UpsertFeed(ctx, srv.URL+"/feed", "", "", 1800, now); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)
	rec := &fakeRecorder{}
	cr.SetMetrics(rec)

	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}
	if len(summary.Results) != 1 || summary.Results[0].Status != "ok" {
		t.Fatalf("results = %+v, want a single \"ok\" result", summary.Results)
	}
	if summary.Results[0].Duration <= 0 {
		t.Fatalf("Duration = %v, want > 0", summary.Results[0].Duration)
	}
	if len(rec.observations) != 1 || rec.observations[0] != "ok" {
		t.Fatalf("recorder observations = %v, want [\"ok\"]", rec.observations)
	}
	if len(rec.durations) != 1 || rec.durations[0] <= 0 {
		t.Fatalf("recorder durations = %v, want a single positive duration", rec.durations)
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
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

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

func TestCrawlerHandlesGoneAndBumpsIntervalOnError(t *testing.T) {
	goneSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer goneSrv.Close()

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errSrv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()

	goneID, err := st.UpsertFeed(ctx, goneSrv.URL+"/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed(gone): %v", err)
	}
	errID, err := st.UpsertFeed(ctx, errSrv.URL+"/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed(err): %v", err)
	}

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)
	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}
	if summary.Errors != 2 {
		t.Fatalf("summary.Errors = %d, want 2", summary.Errors)
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	byID := make(map[int64]struct {
		errorCount int64
		interval   int64
	})
	for _, f := range feeds {
		byID[f.ID] = struct {
			errorCount int64
			interval   int64
		}{f.ErrorCount, f.FetchIntervalSec}
	}

	if got := byID[goneID].errorCount; got != 20 {
		t.Errorf("410 feed error_count = %d, want 20 (excluded from future crawls)", got)
	}
	if got := byID[errID].errorCount; got != 1 {
		t.Errorf("500 feed error_count = %d, want 1", got)
	}
	if got := byID[errID].interval; got <= 1800 {
		t.Errorf("500 feed fetch_interval_sec = %d, want > 1800 (backed off)", got)
	}
}

// TestCrawlerReportsStatusAndContentTypeOnUnparseableBody regression-covers
// issue feedback: "failed to detect feed type" alone doesn't say why -- a
// 200 response that's actually an HTML login/error page (not XML/JSON feed
// content) should have its status code and Content-Type folded into the
// error message so that's diagnosable without re-fetching the URL by hand.
func TestCrawlerReportsStatusAndContentTypeOnUnparseableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>not a feed</body></html>"))
	}))
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, srv.URL+"/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)
	res, err := cr.CrawlFeed(ctx, feedID)
	if err != nil {
		t.Fatalf("CrawlFeed: %v", err)
	}
	if res.Err == nil {
		t.Fatal("res.Err = nil, want a parse error")
	}
	if res.Internal {
		t.Errorf("res.Internal = true, want false (this is the publisher's fault, not feedla's)")
	}
	msg := res.Err.Error()
	if !strings.Contains(msg, "status=200") {
		t.Errorf("error message %q doesn't mention status=200", msg)
	}
	if !strings.Contains(msg, "text/html") {
		t.Errorf("error message %q doesn't mention the text/html content-type", msg)
	}
}

// TestCrawlerTreatsStoreWriteFailureAsInternal regression-covers the
// external/internal split: a feedla-side store failure (simulated here by
// closing the write DB out from under a successful fetch+parse) must be
// reported as FeedResult.Internal, must NOT be recorded on the feed's own
// error_count/last_error (that's reserved for the feed publisher's own
// failures), and must show up in RecentInternalErrors so it's not silently
// lost.
func TestCrawlerTreatsStoreWriteFailureAsInternal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	// Simulate a feedla-side store outage: the fetch and parse below still
	// succeed (they don't touch the DB at all), only the write that follows
	// fails.
	if err := st.Write.Close(); err != nil {
		t.Fatalf("Write.Close: %v", err)
	}

	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(summary.Results))
	}
	res := summary.Results[0]
	if res.Err == nil {
		t.Fatal("res.Err = nil, want non-nil (the store write should have failed)")
	}
	if !res.Internal {
		t.Errorf("res.Internal = false, want true: %v", res.Err)
	}

	f, err := st.GetFeed(ctx, feedID)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if f.ErrorCount != 0 {
		t.Errorf("feed.ErrorCount = %d, want 0 (an internal failure must not be blamed on the feed)", f.ErrorCount)
	}
	if f.LastError != nil {
		t.Errorf("feed.LastError = %q, want nil", *f.LastError)
	}

	entries := cr.RecentInternalErrors()
	if len(entries) != 1 {
		t.Fatalf("len(RecentInternalErrors()) = %d, want 1", len(entries))
	}
	if entries[0].FeedID != feedID {
		t.Errorf("entries[0].FeedID = %d, want %d", entries[0].FeedID, feedID)
	}
}
