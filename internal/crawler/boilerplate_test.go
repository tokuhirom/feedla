package crawler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
)

// chromeFeedTemplate lists three articles, so one crawl fetches three
// article pages in a row -- enough for the third to benefit from what the
// first two revealed about the site's chrome.
const chromeFeedTemplate = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Archive</title>
<link>%[1]s/</link>
<item><title>One</title><link>%[1]s/msg/1</link><guid>guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate><description>Teaser one.</description></item>
<item><title>Two</title><link>%[1]s/msg/2</link><guid>guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 16:04:05 GMT</pubDate><description>Teaser two.</description></item>
<item><title>Three</title><link>%[1]s/msg/3</link><guid>guid-3</guid>
  <pubDate>Mon, 02 Jan 2006 17:04:05 GMT</pubDate><description>Teaser three.</description></item>
</channel></rss>`

// chromeArticleTemplate mimics the page shape this mechanism exists for: an
// old-style document with omitted end tags, a table-based layout, a long
// site-wide navigation menu, and an article body that is a bare <pre> under
// <body> with no container of its own. Readability scores <body> itself as
// the best candidate on a page like this and hands back the whole site
// chrome as "the article".
const chromeArticleTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN">
<html>
<head><title>%[1]s</title></head>
<BODY bgcolor="#E0E0E0" text="black">
<table width="100%%" border="0">
<tr>
<td><a href="/"><img src="/logo.png" border="0" alt="Example"></a>
<td><div class="nav">
<ul>
<li><a href="/products/">Products and services offered by the example organization</a>
<ul>
<li><a href="/products/server/">Server distribution maintained by the same project</a>
<li><a href="/products/tools/">Command line tools for administrators and operators</a>
<li><a href="/products/library/">Libraries used by the tools listed above</a>
</ul>
<li><a href="/services/">Consulting services and commercial support options</a>
<li><a href="/publications/">Articles, presentations and other published material</a>
<li><a href="/resources/">Mailing lists, community wiki and source repositories</a>
<li><a href="/news">Recent news and release announcements from the project</a>
</ul>
</div>
</table>
<a href="%[2]d">[&lt;prev]</a> <a href="%[3]d">[next&gt;]</a> <a href=".">[index]</a>
<pre>%[4]s</pre>
<p><a href="/generator/">Powered by an example archive generator</a> - <a href="/lists/">more mailing lists</a>
<p>Confused about mailing lists and how they are used? Read the introduction
and check out the guidelines on formatting messages properly.
</body>
</html>`

func newChromeArticleServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, chromeFeedTemplate, srv.URL)
	})
	for i := 1; i <= 3; i++ {
		n := i
		mux.HandleFunc(fmt.Sprintf("/msg/%d", n), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			body := strings.Repeat(fmt.Sprintf("Body of message %d, a paragraph of plain text long enough to pass the minimum length check. ", n), 4)
			_, _ = fmt.Fprintf(w, chromeArticleTemplate, fmt.Sprintf("Message %d", n), n-1, n+1, body)
		})
	}
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// bodyContaining returns the stored body of the entry whose text contains
// marker.
func bodyContaining(t *testing.T, bodies []string, marker string) string {
	t.Helper()
	for _, b := range bodies {
		if strings.Contains(b, marker) {
			return b
		}
	}
	t.Fatalf("no entry body contains %q; bodies = %q", marker, bodies)
	return ""
}

func TestFulltextStripsChromeRepeatedAcrossArticles(t *testing.T) {
	srv := newChromeArticleServer(t)

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
	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)
	if _, err := cr.CrawlFeed(ctx, feedID); err != nil {
		t.Fatalf("CrawlFeed: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	bodies := make([]string, len(entries))
	for i, e := range entries {
		bodies[i] = e.Body
	}

	const navText = "Products and services offered by the example organization"

	// The first page has nothing to compare against, so it still shows the
	// problem this mechanism addresses -- asserted here so the fixture keeps
	// reproducing it if Readability's own behavior changes.
	first := bodyContaining(t, bodies, "Body of message 1")
	if !strings.Contains(first, navText) {
		t.Log("first article no longer pulls in the site navigation; the fixture may no longer reproduce the original problem")
	}

	third := bodyContaining(t, bodies, "Body of message 3")
	if strings.Contains(third, navText) {
		t.Errorf("third article kept the navigation shared by every page:\n%s", third)
	}
	if strings.Contains(third, "Powered by an example archive generator") {
		t.Errorf("third article kept the shared footer:\n%s", third)
	}
	if !strings.Contains(third, "Body of message 3") {
		t.Errorf("third article lost its own body:\n%s", third)
	}
}

func TestSelectorSourceStripsChromeRepeatedAcrossArticles(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, selectorListingHTML([]int{1, 2, 3}))
	})
	for i := 1; i <= 3; i++ {
		n := i
		mux.HandleFunc(fmt.Sprintf("/article/%d", n), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			body := strings.Repeat(fmt.Sprintf("Body of message %d, a paragraph of plain text long enough to pass the minimum length check. ", n), 4)
			_, _ = fmt.Fprintf(w, chromeArticleTemplate, fmt.Sprintf("Message %d", n), n-1, n+1, body)
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	st := openTestStore(t)
	now := time.Now()
	feedID := newSelectorSubscription(t, st, srv.URL, `{"item_selector": "article"}`, now)

	cr := crawler.New(st, newTestFetcher(), 4, 0, 0)
	if _, err := cr.CrawlFeed(ctx, feedID); err != nil {
		t.Fatalf("CrawlFeed: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	bodies := make([]string, len(entries))
	for i, e := range entries {
		bodies[i] = e.Body
	}

	third := bodyContaining(t, bodies, "Body of message 3")
	if strings.Contains(third, "Products and services offered by the example organization") {
		t.Errorf("third article kept the navigation shared by every page:\n%s", third)
	}
	if !strings.Contains(third, "Body of message 3") {
		t.Errorf("third article lost its own body:\n%s", third)
	}
}

func TestCrawlWithNoNewEntriesLeavesBoilerplateStateAlone(t *testing.T) {
	srv := newChromeArticleServer(t)

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
		t.Fatalf("first CrawlFeed: %v", err)
	}
	if _, err := st.GetFeedBoilerplate(ctx, feedID); err != nil {
		t.Fatalf("GetFeedBoilerplate after the first crawl: %v", err)
	}

	// A crawl that finds no new entries fetches no article pages, and so
	// must not rewrite the row (the same rule the selector design puts on
	// scrape_sources.state).
	sentinel := json.RawMessage(`{"v":1,"pages":7,"counts":{}}`)
	if err := st.PutFeedBoilerplate(ctx, feedID, sentinel, now); err != nil {
		t.Fatalf("PutFeedBoilerplate: %v", err)
	}
	if _, err := cr.CrawlFeed(ctx, feedID); err != nil {
		t.Fatalf("second CrawlFeed: %v", err)
	}
	got, err := st.GetFeedBoilerplate(ctx, feedID)
	if err != nil {
		t.Fatalf("GetFeedBoilerplate after the second crawl: %v", err)
	}
	if string(got) != string(sentinel) {
		t.Errorf("state = %s, want it untouched by a crawl that fetched nothing", got)
	}
}
