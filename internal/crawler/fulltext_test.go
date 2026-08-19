package crawler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
)

const fulltextFeedTemplate = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Truncated Feed</title>
<link>%[1]s/</link>
<item>
  <title>Article One</title>
  <link>%[1]s/article/1</link>
  <guid>guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>A short teaser. Read more.</description>
</item>
</channel></rss>`

const fulltextArticleTemplate = `<!DOCTYPE html><html><head><title>Article One</title></head>
<body>
<nav><a href="/">Home</a></nav>
<article>
<h1>Article One</h1>
<p>%s</p>
<p>A second paragraph of substantive prose continues the article, well above the length of the feed's own teaser.</p>
</article>
<footer><p>Copyright Example Corp.</p></footer>
</body></html>`

func newFulltextTestServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	articleRequests := 0
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, fulltextFeedTemplate, srv.URL)
	})
	mux.HandleFunc("/article/1", func(w http.ResponseWriter, r *http.Request) {
		articleRequests++
		w.Header().Set("Content-Type", "text/html")
		body := strings.Repeat("This is the real extracted article body, long enough that Readability's scoring favors it over the surrounding navigation and footer noise. ", 6)
		_, _ = fmt.Fprintf(w, fulltextArticleTemplate, body)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &articleRequests
}

func TestFulltextEnabledFeedExtractsEntryBody(t *testing.T) {
	srv, articleRequests := newFulltextTestServer(t)

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, srv.URL+"/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := st.EnableFeedFulltext(ctx, feedID, testUserID, now); err != nil {
		t.Fatalf("EnableFeedFulltext: %v", err)
	}

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)

	if _, err := cr.CrawlFeed(ctx, feedID); err != nil {
		t.Fatalf("CrawlFeed: %v", err)
	}
	if *articleRequests != 1 {
		t.Fatalf("article requests after first crawl = %d, want 1", *articleRequests)
	}

	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Body, "real extracted article body") {
		t.Errorf("entry body = %q, want it to contain the extracted article text", entries[0].Body)
	}
	if strings.Contains(entries[0].Body, "A short teaser") {
		t.Errorf("entry body = %q, want the feed's teaser to be replaced", entries[0].Body)
	}

	// Second crawl of the same feed: the entry already exists, so the
	// article page must not be fetched again.
	if _, err := cr.CrawlFeed(ctx, feedID); err != nil {
		t.Fatalf("second CrawlFeed: %v", err)
	}
	if *articleRequests != 1 {
		t.Errorf("article requests after second crawl = %d, want still 1 (no re-fetch of an existing entry)", *articleRequests)
	}
}

func TestFulltextDisabledFeedKeepsFeedSummary(t *testing.T) {
	srv, articleRequests := newFulltextTestServer(t)

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, srv.URL+"/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	// Fulltext left disabled -- the crawler must not touch /article/1 at all.

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)
	if _, err := cr.CrawlFeed(ctx, feedID); err != nil {
		t.Fatalf("CrawlFeed: %v", err)
	}
	if *articleRequests != 0 {
		t.Errorf("article requests = %d, want 0 when fulltext is disabled", *articleRequests)
	}

	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "A short teaser") {
		t.Errorf("entries = %+v, want the untouched feed teaser body", entries)
	}
}
