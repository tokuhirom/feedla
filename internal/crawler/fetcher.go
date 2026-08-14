package crawler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultTimeout      = 60 * time.Second
	defaultMaxBodyBytes = 10 << 20 // 10 MiB
)

// FetcherConfig configures a Fetcher. Zero values fall back to the defaults
// documented in README.md.
type FetcherConfig struct {
	UserAgent    string
	Timeout      time.Duration
	MaxBodyBytes int64
	// DialContext overrides the dialer used for outbound connections. Nil
	// means the SSRF-safe dialer (see dialer.go); tests point this at
	// net.Dialer{}.DialContext so they can hit httptest.Server on loopback.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Fetcher performs conditional-GET HTTP fetches for feed URLs.
type Fetcher struct {
	client       *http.Client
	userAgent    string
	maxBodyBytes int64
}

// NewFetcher builds a Fetcher from cfg.
func NewFetcher(cfg FetcherConfig) *Fetcher {
	dial := cfg.DialContext
	if dial == nil {
		dial = safeDialContext()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}

	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           dial,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   2,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 20 * time.Second,
				ForceAttemptHTTP2:     true,
			},
			CheckRedirect: limitRedirects(5),
		},
		userAgent:    cfg.UserAgent,
		maxBodyBytes: maxBody,
	}
}

// FetchResult is the outcome of one conditional GET.
type FetchResult struct {
	StatusCode   int
	NotModified  bool // shorthand for StatusCode == http.StatusNotModified
	Body         []byte
	ETag         string
	LastModified string
}

// Fetch performs a conditional GET against feedURL, sending If-None-Match /
// If-Modified-Since when etag/lastModified are non-empty. It returns a
// result for any response feedla received (including 4xx/5xx — callers
// decide how to record those); err is only set when the request couldn't be
// completed at all (blocked address, network error, oversized body, ...).
func (f *Fetcher) Fetch(ctx context.Context, feedURL, etag, lastModified string) (*FetchResult, error) {
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil, fmt.Errorf("crawler: parse feed url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("crawler: unsupported scheme %q", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("crawler: build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, application/json;q=0.9, */*;q=0.8")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crawler: fetch %s: %w", feedURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	result := &FetchResult{
		StatusCode:   resp.StatusCode,
		NotModified:  resp.StatusCode == http.StatusNotModified,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}
	if result.NotModified {
		return result, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("crawler: read body of %s: %w", feedURL, err)
	}
	if int64(len(body)) > f.maxBodyBytes {
		return nil, fmt.Errorf("crawler: response body of %s exceeds %d bytes", feedURL, f.maxBodyBytes)
	}
	result.Body = body
	return result, nil
}

func limitRedirects(n int) func(req *http.Request, via []*http.Request) error {
	return func(_ *http.Request, via []*http.Request) error {
		if len(via) >= n {
			return fmt.Errorf("crawler: stopped after %d redirects", n)
		}
		return nil
	}
}
