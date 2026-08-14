package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/web"
)

func TestHandler_ServesIndex(t *testing.T) {
	h, err := web.Handler()
	if err != nil {
		t.Fatalf("web.Handler() error = %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp == "" {
		t.Errorf("GET / missing Content-Security-Policy header")
	}
}

func TestHandler_ServesStaticAsset(t *testing.T) {
	h, err := web.Handler()
	if err != nil {
		t.Fatalf("web.Handler() error = %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/favicon.svg")
	if err != nil {
		t.Fatalf("GET /favicon.svg error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /favicon.svg status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandler_FallsBackToIndexForUnknownPath(t *testing.T) {
	h, err := web.Handler()
	if err != nil {
		t.Fatalf("web.Handler() error = %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/some/client/route")
	if err != nil {
		t.Fatalf("GET /some/client/route error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /some/client/route status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /some/client/route Content-Type = %q, want text/html", ct)
	}
}
