// Tests for the FR_QUOTA_* resource/rate limits from docs/multi-user-
// design.md's リソース制限・abuse 対策 section. Each test sets a tiny quota
// via api.Options{Quota: ...} so the limit is reachable in a handful of
// requests instead of thousands.
package api_test

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/config"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

func TestSubscriptionCountQuota(t *testing.T) {
	apiSrv, feedSrv, client := newTestServerWithOptions(t, api.Options{
		Quota: config.Quota{MaxSubscriptions: 1, FeedAddPerHour: 100},
	})

	feedSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, testFeedXML)
	}))
	t.Cleanup(feedSrv2.Close)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("first subscribe status = %d, want 201: %s", resp.StatusCode, body)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv2.URL})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("second subscribe (over MaxSubscriptions=1) status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestFeedAddRateLimit(t *testing.T) {
	apiSrv, feedSrv, client := newTestServerWithOptions(t, api.Options{
		Quota: config.Quota{MaxSubscriptions: 100, FeedAddPerHour: 1},
	})

	feedSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, testFeedXML)
	}))
	t.Cleanup(feedSrv2.Close)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("first subscribe status = %d, want 201: %s", resp.StatusCode, body)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv2.URL})
	if resp.StatusCode != http.StatusTooManyRequests {
		body, _ := decodeBody(resp)
		t.Fatalf("second subscribe (over FeedAddPerHour=1) status = %d, want 429: %s", resp.StatusCode, body)
	}
}

func TestPinCountQuota(t *testing.T) {
	apiSrv, feedSrv, client := newTestServerWithOptions(t, api.Options{
		Quota: config.Quota{MaxPins: 1},
	})

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	resp, err := client.Get(fmt.Sprintf("%s/api/v1/subscriptions/%d/entries?unread=1", apiSrv.URL, feedID))
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	var entriesResp struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, resp, &entriesResp)
	if len(entriesResp.Entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entriesResp.Entries))
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/pins", map[string]int64{"entry_id": entriesResp.Entries[0].ID})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("first pin status = %d, want 201: %s", resp.StatusCode, body)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/pins", map[string]int64{"entry_id": entriesResp.Entries[1].ID})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("second pin (over MaxPins=1) status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestIgnoreWordCountQuota(t *testing.T) {
	apiSrv, _, client := newTestServerWithOptions(t, api.Options{
		Quota: config.Quota{MaxIgnoreWords: 1},
	})

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/ignore_words", map[string]string{"word": "spam"})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("first ignore word status = %d, want 201: %s", resp.StatusCode, body)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/ignore_words", map[string]string{"word": "junk"})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("second ignore word (over MaxIgnoreWords=1) status = %d, want 400: %s", resp.StatusCode, body)
	}
}

// newTestServerWithPageOptions is scrape_sources_test.go's
// newTestServerWithPage plus an api.Options override, for the scrape-source
// quota tests below.
func newTestServerWithPageOptions(t *testing.T, initialHTML string, opts api.Options) (apiSrv, pageSrv *httptest.Server, client *http.Client, st *store.Store) {
	t.Helper()

	pageSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, initialHTML)
	}))
	t.Cleanup(pageSrv.Close)

	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	fetcher := crawler.NewFetcher(crawler.FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     crawler.NewHostSemaphore(4, 0),
	})
	cr := crawler.New(st, fetcher, 4, 0, 0)

	apiSrv = httptest.NewServer(api.NewHandler(st, cr, fetcher, nil, opts))
	t.Cleanup(apiSrv.Close)
	client = loginTestClient(t, apiSrv.URL)
	return apiSrv, pageSrv, client, st
}

func TestScrapeSourceCountQuota(t *testing.T) {
	apiSrv, pageSrv, client, _ := newTestServerWithPageOptions(t, `<html><body><p>本文です。</p></body></html>`, api.Options{
		Quota: config.Quota{MaxSubscriptions: 100, MaxScrapeSources: 1, FeedAddPerHour: 100},
	})

	pageSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><body><p>別のページです。</p></body></html>`)
	}))
	t.Cleanup(pageSrv2.Close)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]string{"url": pageSrv.URL})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("first scrape source status = %d, want 201: %s", resp.StatusCode, body)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]string{"url": pageSrv2.URL})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("second scrape source (over MaxScrapeSources=1) status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestPreviewRateLimit(t *testing.T) {
	apiSrv, pageSrv, client, _ := newTestServerWithPageOptions(t, `<html><body><p>本文です。</p></body></html>`, api.Options{
		Quota: config.Quota{MaxSubscriptions: 100, MaxScrapeSources: 100, FeedAddPerHour: 100, PreviewPerHour: 1},
	})

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]string{"url": pageSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)

	listResp, err := client.Get(apiSrv.URL + "/api/v1/scrape_sources")
	if err != nil {
		t.Fatalf("GET scrape_sources: %v", err)
	}
	var list struct {
		ScrapeSources []struct {
			ID int64 `json:"id"`
		} `json:"scrape_sources"`
	}
	decodeJSON(t, listResp, &list)
	if len(list.ScrapeSources) != 1 {
		t.Fatalf("scrape_sources = %+v, want one entry", list.ScrapeSources)
	}
	previewURL := fmt.Sprintf("%s/api/v1/scrape_sources/%d/preview", apiSrv.URL, list.ScrapeSources[0].ID)

	resp = postJSON(t, client, previewURL, nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("first preview status = %d, want 200: %s", resp.StatusCode, body)
	}

	resp = postJSON(t, client, previewURL, nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		body, _ := decodeBody(resp)
		t.Fatalf("second preview (over PreviewPerHour=1) status = %d, want 429: %s", resp.StatusCode, body)
	}
}

// TestRefreshScopedToOwnSubscription covers the ownership gap
// handleRefresh had: a manual refresh must be limited to the caller's own
// subscriptions, since a feed is shared across every subscriber and forcing
// a crawl of one affects them all.
func TestRefreshScopedToOwnSubscription(t *testing.T) {
	apiSrv, feedSrv, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)
	refreshURL := fmt.Sprintf("%s/api/v1/subscriptions/%d/refresh", apiSrv.URL, feedID)
	if resp := postJSON(t, other, refreshURL, nil); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-subscriber refresh status = %d, want 404: %s", resp.StatusCode, body)
	}

	if resp := postJSON(t, owner, refreshURL, nil); resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("owner refresh status = %d, want 200: %s", resp.StatusCode, body)
	}
}

func TestRefreshRateLimit(t *testing.T) {
	apiSrv, feedSrv, client, _ := newTestServerWithStoreAndOptions(t, api.Options{
		Quota: config.Quota{RefreshPerHour: 1},
	})

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID
	refreshURL := fmt.Sprintf("%s/api/v1/subscriptions/%d/refresh", apiSrv.URL, feedID)

	if resp := postJSON(t, client, refreshURL, nil); resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("first refresh status = %d, want 200: %s", resp.StatusCode, body)
	}
	if resp := postJSON(t, client, refreshURL, nil); resp.StatusCode != http.StatusTooManyRequests {
		body, _ := decodeBody(resp)
		t.Fatalf("second refresh (over RefreshPerHour=1) status = %d, want 429: %s", resp.StatusCode, body)
	}
}

// newTestServerWithStoreAndOptions is newTestServerWithOptions plus the
// underlying *store.Store, needed by tests that create a second user via
// createTestUser.
func newTestServerWithStoreAndOptions(t *testing.T, opts api.Options) (apiSrv, feedSrv *httptest.Server, client *http.Client, st *store.Store) {
	t.Helper()

	feedSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, testFeedXML)
	}))
	t.Cleanup(feedSrv.Close)

	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	fetcher := crawler.NewFetcher(crawler.FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     crawler.NewHostSemaphore(4, 0),
	})
	cr := crawler.New(st, fetcher, 4, 0, 0)

	apiSrv = httptest.NewServer(api.NewHandler(st, cr, fetcher, nil, opts))
	t.Cleanup(apiSrv.Close)
	client = loginTestClient(t, apiSrv.URL)
	return apiSrv, feedSrv, client, st
}

func TestOPMLImportMaxFeedsQuota(t *testing.T) {
	apiSrv, _, client := newTestServerWithOptions(t, api.Options{
		Quota: config.Quota{OPMLMaxFeeds: 1},
	})

	const twoFeedOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
  <head><title>subscriptions</title></head>
  <body>
    <outline text="Feed A" title="Feed A" type="rss"
      xmlUrl="https://a.example.com/feed" htmlUrl="https://a.example.com/"/>
    <outline text="Feed B" title="Feed B" type="rss"
      xmlUrl="https://b.example.com/feed" htmlUrl="https://b.example.com/"/>
  </body>
</opml>`

	resp := doWithOrigin(t, client, http.MethodPost, apiSrv.URL+"/api/v1/opml", "text/x-opml", bytes.NewBufferString(twoFeedOPML))
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("import over OPMLMaxFeeds=1 status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestAPIPerMinuteRateLimit(t *testing.T) {
	apiSrv, _, client := newTestServerWithOptions(t, api.Options{
		Quota: config.Quota{APIPerMinute: 2},
	})

	get := func() int {
		resp, err := client.Get(apiSrv.URL + "/api/v1/subscriptions")
		if err != nil {
			t.Fatalf("GET subscriptions: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	for i := range 2 {
		if status := get(); status != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, status)
		}
	}
	if status := get(); status != http.StatusTooManyRequests {
		t.Fatalf("3rd request (over APIPerMinute=2) status = %d, want 429", status)
	}
}
