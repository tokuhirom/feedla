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

const sampleHTML = `<!DOCTYPE html>
<html><head>
<title>Example Site</title>
<link rel="alternate" type="application/rss+xml" title="Example RSS" href="/feed.rss">
<link rel="alternate" type="application/atom+xml" title="Example Atom" href="https://other.example.com/atom.xml">
<link rel="stylesheet" type="text/css" href="/style.css">
</head><body>hello</body></html>`

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

func TestDiscoverFeedFromHTMLLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(sampleHTML))
	}))
	defer srv.Close()

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
	if candidates[1].FeedURL != "https://other.example.com/atom.xml" {
		t.Errorf("candidates[1].FeedURL = %q, want absolute link kept as-is", candidates[1].FeedURL)
	}
}

func TestDiscoverFeedFallbackGuessedURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r/example/.rss" {
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(sampleRSS))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>No feed links here</title></head></html>`))
	}))
	defer srv.Close()

	candidates, err := feed.DiscoverFeed(t.Context(), testFetcher(), srv.URL+"/r/example/")
	if err != nil {
		t.Fatalf("DiscoverFeed: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1: %+v", len(candidates), candidates)
	}
	if want := srv.URL + "/r/example/.rss"; candidates[0].FeedURL != want {
		t.Errorf("FeedURL = %q, want %q", candidates[0].FeedURL, want)
	}
	if candidates[0].Title != "Direct Feed" {
		t.Errorf("Title = %q, want %q", candidates[0].Title, "Direct Feed")
	}
}

func TestDiscoverFeedSkipsFallbackWhenHTMLLinksFound(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(sampleHTML))
	}))
	defer srv.Close()

	if _, err := feed.DiscoverFeed(t.Context(), testFetcher(), srv.URL+"/"); err != nil {
		t.Fatalf("DiscoverFeed: %v", err)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1 (guess fallback should be skipped once HTML links are found)", requests)
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
