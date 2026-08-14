package crawler_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
)

func testFetcherConfig() crawler.FetcherConfig {
	return crawler.FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     crawler.NewHostSemaphore(4, 0),
	}
}

func TestFetcherFollowsPermanentRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := crawler.NewFetcher(testFetcherConfig())
	result, err := f.Fetch(t.Context(), srv.URL+"/old", "", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if !result.PermanentRedirect {
		t.Error("PermanentRedirect = false, want true after a 301")
	}
	if result.FinalURL != srv.URL+"/new" {
		t.Errorf("FinalURL = %q, want %q", result.FinalURL, srv.URL+"/new")
	}
	if string(result.Body) != "ok" {
		t.Errorf("Body = %q, want %q", result.Body, "ok")
	}
}

func TestFetcherFollowsTemporaryRedirectWithoutFlagging(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusFound)
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := crawler.NewFetcher(testFetcherConfig())
	result, err := f.Fetch(t.Context(), srv.URL+"/old", "", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.PermanentRedirect {
		t.Error("PermanentRedirect = true after a 302, want false")
	}
}

func TestFetcherTooManyRedirectsFails(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
	}))
	defer srv.Close()

	f := crawler.NewFetcher(testFetcherConfig())
	if _, err := f.Fetch(t.Context(), srv.URL+"/loop", "", ""); err == nil {
		t.Fatal("Fetch: want error for an infinite redirect loop")
	}
}

func TestFetcherParsesRetryAfterSeconds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := crawler.NewFetcher(testFetcherConfig())
	result, err := f.Fetch(t.Context(), srv.URL, "", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want 503", result.StatusCode)
	}
	if result.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want 5s", result.RetryAfter)
	}
}
