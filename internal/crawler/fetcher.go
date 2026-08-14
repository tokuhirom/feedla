package crawler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultTimeout      = 60 * time.Second
	defaultMaxBodyBytes = 10 << 20 // 10 MiB
	maxRedirectHops     = 5
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
	// HostSem enforces per-host concurrency/politeness limits. Nil means a
	// default HostSemaphore(2 concurrent, 1s gap) is created.
	HostSem *HostSemaphore
}

// Fetcher performs conditional-GET HTTP fetches for feed URLs, following
// redirects itself (rather than via http.Client's automatic following) so
// it can tell a permanent redirect (301/308) apart from a temporary one.
type Fetcher struct {
	client       *http.Client
	userAgent    string
	maxBodyBytes int64
	hostSem      *HostSemaphore
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
	hostSem := cfg.HostSem
	if hostSem == nil {
		hostSem = NewHostSemaphore(defaultHostConcurrency, defaultHostMinGap)
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
			// We follow redirects ourselves (see Fetch) so we can tell
			// permanent redirects apart from temporary ones.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		userAgent:    cfg.UserAgent,
		maxBodyBytes: maxBody,
		hostSem:      hostSem,
	}
}

// FetchResult is the outcome of one conditional GET (after following any
// redirects).
type FetchResult struct {
	StatusCode        int
	NotModified       bool // shorthand for StatusCode == http.StatusNotModified
	Body              []byte
	ETag              string
	LastModified      string
	RetryAfter        time.Duration // parsed from a Retry-After response header, if present
	FinalURL          string        // URL the response actually came from, after following redirects
	PermanentRedirect bool          // true if any hop in the chain was a 301/308 and FinalURL != the requested URL
}

// Fetch performs a conditional GET against feedURL, sending If-None-Match /
// If-Modified-Since when etag/lastModified are non-empty, following up to
// maxRedirectHops redirects. It returns a result for any response feedla
// received (including 4xx/5xx — callers decide how to record those); err is
// only set when the request couldn't be completed at all (blocked address,
// network error, oversized body, too many redirects, ...).
func (f *Fetcher) Fetch(ctx context.Context, feedURL, etag, lastModified string) (*FetchResult, error) {
	currentURL := feedURL
	permanentRedirect := false

	for hop := 0; ; hop++ {
		if hop > maxRedirectHops {
			return nil, fmt.Errorf("crawler: stopped after %d redirects", maxRedirectHops)
		}

		resp, release, err := f.doRequest(ctx, currentURL, etag, lastModified)
		if err != nil {
			return nil, err
		}

		if loc := redirectLocation(resp); loc != "" {
			if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusPermanentRedirect {
				permanentRedirect = true
			}
			next, resolveErr := resolveRedirect(currentURL, loc)
			_ = resp.Body.Close()
			release()
			if resolveErr != nil {
				return nil, fmt.Errorf("crawler: bad redirect location %q: %w", loc, resolveErr)
			}
			currentURL = next
			continue
		}

		result, buildErr := f.buildResult(resp)
		_ = resp.Body.Close()
		release()
		if buildErr != nil {
			return nil, buildErr
		}
		result.FinalURL = currentURL
		result.PermanentRedirect = permanentRedirect && currentURL != feedURL
		return result, nil
	}
}

func (f *Fetcher) doRequest(ctx context.Context, target, etag, lastModified string) (*http.Response, func(), error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, nil, fmt.Errorf("crawler: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, nil, fmt.Errorf("crawler: unsupported scheme %q", u.Scheme)
	}

	release, err := f.hostSem.Acquire(ctx, u.Host)
	if err != nil {
		return nil, nil, fmt.Errorf("crawler: host semaphore: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("crawler: build request: %w", err)
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
		release()
		return nil, nil, fmt.Errorf("crawler: fetch %s: %w", target, err)
	}
	return resp, release, nil
}

func (f *Fetcher) buildResult(resp *http.Response) (*FetchResult, error) {
	result := &FetchResult{
		StatusCode:   resp.StatusCode,
		NotModified:  resp.StatusCode == http.StatusNotModified,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		RetryAfter:   parseRetryAfter(resp.Header.Get("Retry-After")),
	}
	if result.NotModified {
		return result, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("crawler: read body: %w", err)
	}
	if int64(len(body)) > f.maxBodyBytes {
		return nil, fmt.Errorf("crawler: response body exceeds %d bytes", f.maxBodyBytes)
	}
	result.Body = body
	return result, nil
}

func redirectLocation(resp *http.Response) string {
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return resp.Header.Get("Location")
	default:
		return ""
	}
}

func resolveRedirect(base, loc string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	l, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(l).String(), nil
}

// parseRetryAfter accepts both forms RFC 9110 allows: a delta in seconds or
// an HTTP-date. It returns 0 (meaning "not present / already past") when v
// is empty, unparseable, or names a point in the past.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
