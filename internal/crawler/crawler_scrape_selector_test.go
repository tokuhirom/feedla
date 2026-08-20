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

const selectorArticleTemplate = `<!DOCTYPE html><html><head><title>%[1]s</title></head>
<body>
<nav><a href="/list">Back</a></nav>
<article>
<h1>%[1]s</h1>
<p>%[2]s</p>
<p>A second paragraph of substantive prose continues the article, well above the extraction length threshold.</p>
</article>
</body></html>`

func selectorArticleBody(title string) string {
	text := strings.Repeat("This is the real extracted article body, long enough for Readability to favor it over surrounding noise. ", 4)
	return fmt.Sprintf(selectorArticleTemplate, title, text)
}

func selectorListingHTML(articleNums []int) string {
	var b strings.Builder
	b.WriteString("<html><head><title>Listing</title></head><body>")
	for _, n := range articleNums {
		fmt.Fprintf(&b, `<article><a href="/article/%d">Article %d</a></article>`, n, n)
	}
	b.WriteString("</body></html>")
	return b.String()
}

// newSelectorSubscription sets up a selector-watched feed row + scrape_sources
// row for srv.URL+"/list", mirroring what POST /api/v1/scrape_sources will do
// once the API layer exists (a later step of the selector work breakdown) --
// CreateScrapeSource itself is exercised directly here since that endpoint
// isn't wired up yet.
func newSelectorSubscription(t *testing.T, st *store.Store, srvURL, config string, now time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	feedID, err := st.UpsertFeed(ctx, crawler.SelectorPrefix+srvURL+"/list", "", "", 3600, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if _, err := st.CreateScrapeSource(ctx, testUserID, feedID, "selector", srvURL+"/list", []byte(config), now); err != nil {
		t.Fatalf("CreateScrapeSource: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	return feedID
}

func TestCrawlerSelectorSourceInitialAndIncremental(t *testing.T) {
	var articleNums atomic.Value
	articleNums.Store([]int{1, 2, 3})
	var req1, req2, req3, req4 int32

	mux := http.NewServeMux()
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, selectorListingHTML(articleNums.Load().([]int)))
	})
	counters := map[int]*int32{1: &req1, 2: &req2, 3: &req3, 4: &req4}
	for n, counter := range counters {
		title := fmt.Sprintf("Article %d", n)
		c := counter
		mux.HandleFunc(fmt.Sprintf("/article/%d", n), func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(c, 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, selectorArticleBody(title))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	feedID := newSelectorSubscription(t, st, srv.URL, `{"item_selector": "article"}`, now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll (initial): %v", err)
	}
	if summary.Errors != 0 || summary.NewEntries != 3 {
		t.Fatalf("initial crawl summary = %+v, want 0 errors / 3 new entries", summary)
	}

	summary, err = cr.CrawlAll(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll (unchanged): %v", err)
	}
	if summary.Errors != 0 || summary.NewEntries != 0 {
		t.Fatalf("unchanged crawl summary = %+v, want 0 errors / 0 new entries", summary)
	}

	articleNums.Store([]int{1, 2, 3, 4})
	summary, err = cr.CrawlAll(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll (grew): %v", err)
	}
	if summary.Errors != 0 || summary.NewEntries != 1 {
		t.Fatalf("grew crawl summary = %+v, want 0 errors / 1 new entry", summary)
	}

	for n, counter := range counters {
		want := int32(1)
		if got := atomic.LoadInt32(counter); got != want {
			t.Errorf("article %d requested %d times, want %d (existing articles must not be re-fetched)", n, got, want)
		}
	}

	count, err := st.CountEntries(ctx, feedID)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 4 {
		t.Fatalf("CountEntries = %d, want 4", count)
	}
}

func TestCrawlerSelectorSourceArticleFetchFailureRetriesThenGivesUp(t *testing.T) {
	var requests int32
	mux := http.NewServeMux()
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, selectorListingHTML([]int{1}))
	})
	mux.HandleFunc("/article/1", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	feedID := newSelectorSubscription(t, st, srv.URL, `{"item_selector": "article"}`, now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	// Crawls 1 and 2: article fetch fails, not yet at the retry limit -> no
	// entry created either time.
	for i, at := range []time.Time{now, now.Add(time.Hour)} {
		summary, err := cr.CrawlAll(ctx, at)
		if err != nil {
			t.Fatalf("CrawlAll #%d: %v", i+1, err)
		}
		if summary.NewEntries != 0 {
			t.Fatalf("crawl %d NewEntries = %d, want 0", i+1, summary.NewEntries)
		}
	}

	// Crawl 3: third failure -> give up, import title-only (§4.5).
	summary, err := cr.CrawlAll(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll #3: %v", err)
	}
	if summary.NewEntries != 1 {
		t.Fatalf("crawl 3 NewEntries = %d, want 1 (title-only import after 3 failures)", summary.NewEntries)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("article requests after crawl 3 = %d, want 3", got)
	}
	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Body != "" {
		t.Fatalf("entries = %+v, want one title-only (empty body) entry", entries)
	}

	// Crawl 4: must not retry a 3rd-failure article again.
	summary, err = cr.CrawlAll(ctx, now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll #4: %v", err)
	}
	if summary.NewEntries != 0 {
		t.Fatalf("crawl 4 NewEntries = %d, want 0", summary.NewEntries)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("article requests after crawl 4 = %d, want still 3 (no more retries)", got)
	}
}

func TestCrawlerSelectorSourceMaxItemsPerCrawl(t *testing.T) {
	var requests int32
	mux := http.NewServeMux()
	var articleNums atomic.Value
	articleNums.Store([]int{1})
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, selectorListingHTML(articleNums.Load().([]int)))
	})
	for n := 1; n <= 4; n++ {
		title := fmt.Sprintf("Article %d", n)
		mux.HandleFunc(fmt.Sprintf("/article/%d", n), func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requests, 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, selectorArticleBody(title))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	feedID := newSelectorSubscription(t, st, srv.URL, `{"item_selector": "article", "max_items_per_crawl": 2}`, now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	// Establish a non-initial state with a single seed article, so the
	// initial-crawl "seal the rest" rule (§4.5) doesn't apply to the
	// max_items_per_crawl trimming this test actually wants to exercise.
	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll (seed): %v", err)
	}
	if summary.NewEntries != 1 {
		t.Fatalf("seed crawl NewEntries = %d, want 1", summary.NewEntries)
	}

	// Three more candidates appear at once; max_items_per_crawl=2 caps this
	// crawl's article fetches, leaving the rest for next time.
	articleNums.Store([]int{1, 2, 3, 4})
	summary, err = cr.CrawlAll(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll (capped): %v", err)
	}
	if summary.NewEntries != 2 {
		t.Fatalf("capped crawl NewEntries = %d, want 2 (max_items_per_crawl=2)", summary.NewEntries)
	}
	if got := atomic.LoadInt32(&requests); got != 3 { // 1 seed + 2 this crawl
		t.Fatalf("article requests after capped crawl = %d, want 3", got)
	}

	// Next crawl: the leftover candidate is picked up.
	summary, err = cr.CrawlAll(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("CrawlAll (leftover): %v", err)
	}
	if summary.NewEntries != 1 {
		t.Fatalf("leftover crawl NewEntries = %d, want 1", summary.NewEntries)
	}
	if got := atomic.LoadInt32(&requests); got != 4 {
		t.Fatalf("article requests after leftover crawl = %d, want 4", got)
	}

	count, err := st.CountEntries(ctx, feedID)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 4 {
		t.Fatalf("CountEntries = %d, want 4", count)
	}
}

func TestCrawlerSelectorSourceRobotsDisallowImportsWithoutFetching(t *testing.T) {
	var articleRequests int32
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
	})
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><body><article><a href="/private/secret">Secret Article</a></article></body></html>`)
	})
	mux.HandleFunc("/private/secret", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&articleRequests, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, selectorArticleBody("Secret Article"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	feedID := newSelectorSubscription(t, st, srv.URL, `{"item_selector": "article"}`, now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}
	if summary.NewEntries != 1 {
		t.Fatalf("NewEntries = %d, want 1 (title-only import despite Disallow)", summary.NewEntries)
	}
	if got := atomic.LoadInt32(&articleRequests); got != 0 {
		t.Fatalf("article requests = %d, want 0 (robots.txt Disallow must prevent the fetch)", got)
	}
	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Secret Article" {
		t.Fatalf("entries = %+v, want one title-only entry titled \"Secret Article\"", entries)
	}
	if entries[0].Body != "" {
		t.Errorf("Body = %q, want empty (disallowed article body must not be fetched)", entries[0].Body)
	}
}

func TestCrawlerSelectorSourceNoMatchIsExternalError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><body><p>no articles here</p></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	newSelectorSubscription(t, st, srv.URL, `{"item_selector": "article.does-not-exist"}`, now)
	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	summary, err := cr.CrawlAll(ctx, now)
	if err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}
	if summary.Errors != 1 {
		t.Fatalf("summary.Errors = %d, want 1 (item_selector matched nothing)", summary.Errors)
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 1 || feeds[0].ErrorCount == 0 {
		t.Fatalf("feeds = %+v, want error_count > 0", feeds)
	}
}
