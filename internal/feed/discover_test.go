package feed_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/feed"
)

func testFetcher() *crawler.Fetcher {
	return crawler.NewFetcher(crawler.FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     crawler.NewHostSemaphore(4, 0),
	})
}

const sampleRSS = `<?xml version="1.0"?>
<rss version="2.0"><channel><title>Direct Feed</title><link>https://example.com/</link></channel></rss>`

func TestDiscoverFeedDirectFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	candidates, err := feed.DiscoverFeed(t.Context(), testFetcher(), srv.URL)
	if err != nil {
		t.Fatalf("DiscoverFeed: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if candidates[0].Title != "Direct Feed" {
		t.Errorf("Title = %q, want %q", candidates[0].Title, "Direct Feed")
	}
	if candidates[0].FeedURL != srv.URL {
		t.Errorf("FeedURL = %q, want %q", candidates[0].FeedURL, srv.URL)
	}
}

// TestDiscoverFeedFromHTMLLinks covers the common case a real site hits:
// the <link title="..."> attribute holds a generic format label (as many
// site frameworks emit) rather than the feed's actual name, so discovery
// must fetch and parse each linked feed to recover its real <title>.
func TestDiscoverFeedFromHTMLLinks(t *testing.T) {
	const rssFeed = `<?xml version="1.0"?>
<rss version="2.0"><channel><title>Example RSS Feed</title><link>https://example.com/</link></channel></rss>`
	const atomFeed = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Example Atom Feed</title></feed>`

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head>
<title>Example Site</title>
<link rel="alternate" type="application/rss+xml" title="RSS 2.0" href="/feed.rss">
<link rel="alternate" type="application/atom+xml" title="Atom" href="` + srv.URL + `/atom.xml">
<link rel="stylesheet" type="text/css" href="/style.css">
</head><body>hello</body></html>`))
	})
	mux.HandleFunc("/feed.rss", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssFeed))
	})
	mux.HandleFunc("/atom.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(atomFeed))
	})

	candidates, err := feed.DiscoverFeed(t.Context(), testFetcher(), srv.URL+"/")
	if err != nil {
		t.Fatalf("DiscoverFeed: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2: %+v", len(candidates), candidates)
	}
	if candidates[0].FeedURL != srv.URL+"/feed.rss" {
		t.Errorf("candidates[0].FeedURL = %q, want relative link resolved against base", candidates[0].FeedURL)
	}
	if candidates[0].Title != "Example RSS Feed" {
		t.Errorf("candidates[0].Title = %q, want the feed's own <title> instead of the generic link title", candidates[0].Title)
	}
	if candidates[1].FeedURL != srv.URL+"/atom.xml" {
		t.Errorf("candidates[1].FeedURL = %q, want absolute link kept as-is", candidates[1].FeedURL)
	}
	if candidates[1].Title != "Example Atom Feed" {
		t.Errorf("candidates[1].Title = %q, want the feed's own <title>", candidates[1].Title)
	}
}

// TestDiscoverFeedFallsBackToPageTitleWhenFeedUnreachable covers the case
// where the linked feed itself can't be fetched/parsed: discovery should
// fall back to the HTML page's own <title>, and since multiple such
// fallback candidates could otherwise collide, suffix it with the feed's
// format so an RSS and an Atom candidate stay distinguishable.
func TestDiscoverFeedFallsBackToPageTitleWhenFeedUnreachable(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head>
<title>Example Site</title>
<link rel="alternate" type="application/rss+xml" title="RSS 2.0" href="/missing.rss">
<link rel="alternate" type="application/atom+xml" title="Atom 1.0" href="/missing.atom">
</head><body>hello</body></html>`))
	})
	// /missing.rss and /missing.atom deliberately unhandled: 404.

	candidates, err := feed.DiscoverFeed(t.Context(), testFetcher(), srv.URL+"/")
	if err != nil {
		t.Fatalf("DiscoverFeed: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2: %+v", len(candidates), candidates)
	}
	if want := "Example Site (RSS)"; candidates[0].Title != want {
		t.Errorf("candidates[0].Title = %q, want %q", candidates[0].Title, want)
	}
	if want := "Example Site (Atom)"; candidates[1].Title != want {
		t.Errorf("candidates[1].Title = %q, want %q", candidates[1].Title, want)
	}
}

func TestDiscoverFeedNoCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>No feeds here</title></head></html>`))
	}))
	defer srv.Close()

	if _, err := feed.DiscoverFeed(t.Context(), testFetcher(), srv.URL); err == nil {
		t.Fatal("DiscoverFeed: want error when no feed can be found")
	}
}
