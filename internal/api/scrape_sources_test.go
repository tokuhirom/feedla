package api_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

// newTestServerWithPage is newTestServer's twin for scrape-source tests: the
// "external site" server returns HTML (mutable via pageHTML.Store), not a
// fixed feed XML body.
func newTestServerWithPage(t *testing.T, initialHTML string) (apiSrv, pageSrv *httptest.Server, pageHTML *atomic.Value, client *http.Client) {
	t.Helper()

	pageHTML = &atomic.Value{}
	pageHTML.Store(initialHTML)
	pageSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, pageHTML.Load().(string))
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

	apiSrv = httptest.NewServer(api.NewHandler(st, cr, fetcher, nil, api.Options{}))
	t.Cleanup(apiSrv.Close)
	client = loginTestClient(t, apiSrv.URL)
	return apiSrv, pageSrv, pageHTML, client
}

func TestCreateScrapeSourceCrawlsImmediately(t *testing.T) {
	apiSrv, pageSrv, _, client := newTestServerWithPage(t, `<html><head><title>日記</title></head><body><p>最初の投稿です。</p></body></html>`)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]string{"url": pageSrv.URL})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("create status = %d, want 201: %s", resp.StatusCode, body)
	}
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	if created.Subscription == nil {
		t.Fatal("want a subscription in the response")
	}
	if created.Subscription.Kind != "pagewatch" {
		t.Errorf("Kind = %q, want pagewatch", created.Subscription.Kind)
	}
	if created.Subscription.FeedURL != pageSrv.URL {
		t.Errorf("FeedURL = %q, want %q (pagewatch: prefix stripped)", created.Subscription.FeedURL, pageSrv.URL)
	}
	if created.Subscription.UnreadCount != 1 {
		t.Errorf("UnreadCount = %d, want 1 (the initial 'monitoring started' entry)", created.Subscription.UnreadCount)
	}

	// Appears in the normal subscription list alongside real feeds.
	resp, err := client.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions: %v", err)
	}
	var list struct {
		Subscriptions []store.SubscriptionView `json:"subscriptions"`
	}
	decodeJSON(t, resp, &list)
	if len(list.Subscriptions) != 1 || list.Subscriptions[0].Kind != "pagewatch" {
		t.Fatalf("subscriptions = %+v, want one pagewatch subscription", list.Subscriptions)
	}
}

func TestCreateScrapeSourceMissingURL(t *testing.T) {
	apiSrv, _, _, client := newTestServerWithPage(t, `<html><body><p>x</p></body></html>`)
	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]string{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateScrapeSourceInvalidConfigRejected(t *testing.T) {
	apiSrv, pageSrv, _, client := newTestServerWithPage(t, `<html><body><p>本文です。</p></body></html>`)
	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]any{
		"url":    pageSrv.URL,
		"config": map[string]any{"ignore_patterns": []string{"(unclosed"}},
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestScrapeSourceGetPatchAndPreview(t *testing.T) {
	apiSrv, pageSrv, _, client := newTestServerWithPage(t, `<html><body><p>本文は変わりません。</p><p>Document ID: abc123</p></body></html>`)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]string{"url": pageSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	// GET by feed id: only feed_id is exposed by the subscription view, but
	// GET /scrape_sources lists every source and lets us find this one's own id.
	resp, err := client.Get(apiSrv.URL + "/api/v1/scrape_sources")
	if err != nil {
		t.Fatalf("GET scrape_sources: %v", err)
	}
	var list struct {
		ScrapeSources []struct {
			ID     int64 `json:"id"`
			FeedID int64 `json:"feed_id"`
		} `json:"scrape_sources"`
	}
	decodeJSON(t, resp, &list)
	if len(list.ScrapeSources) != 1 || list.ScrapeSources[0].FeedID != feedID {
		t.Fatalf("scrape_sources = %+v, want one entry for feed %d", list.ScrapeSources, feedID)
	}
	srcID := list.ScrapeSources[0].ID

	resp, err = client.Get(fmt.Sprintf("%s/api/v1/scrape_sources/%d", apiSrv.URL, srcID))
	if err != nil {
		t.Fatalf("GET scrape_sources/%d: %v", srcID, err)
	}
	var got struct {
		Config    json.RawMessage `json:"config"`
		TargetURL string          `json:"target_url"`
	}
	decodeJSON(t, resp, &got)
	if got.TargetURL != pageSrv.URL {
		t.Errorf("TargetURL = %q, want %q", got.TargetURL, pageSrv.URL)
	}
	if string(got.Config) != "{}" {
		t.Errorf("Config = %s, want default {}", got.Config)
	}

	// PATCH the config to add an ignore_pattern.
	patchResp := patchJSON(t, client, fmt.Sprintf("%s/api/v1/scrape_sources/%d", apiSrv.URL, srcID), map[string]any{
		"config": map[string]any{"ignore_patterns": []string{"Document ID: [A-Za-z0-9]+"}},
	})
	if patchResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(patchResp)
		t.Fatalf("PATCH status = %d, want 200: %s", patchResp.StatusCode, body)
	}

	// Preview should now show the Document ID block as masked.
	previewResp := postJSON(t, client, fmt.Sprintf("%s/api/v1/scrape_sources/%d/preview", apiSrv.URL, srcID), nil)
	if previewResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(previewResp)
		t.Fatalf("preview status = %d, want 200: %s", previewResp.StatusCode, body)
	}
	var preview struct {
		Blocks []struct {
			Text   string `json:"text"`
			Masked bool   `json:"masked"`
		} `json:"blocks"`
	}
	decodeJSON(t, previewResp, &preview)
	if len(preview.Blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(preview.Blocks))
	}
	if preview.Blocks[0].Masked {
		t.Errorf("blocks[0] (%q) masked, want unmasked", preview.Blocks[0].Text)
	}
	if !preview.Blocks[1].Masked {
		t.Errorf("blocks[1] (%q) unmasked, want masked", preview.Blocks[1].Text)
	}
}

func TestScrapeSourceGetNotFound(t *testing.T) {
	apiSrv, _, _, client := newTestServerWithPage(t, `<html><body><p>x</p></body></html>`)
	resp, err := client.Get(apiSrv.URL + "/api/v1/scrape_sources/999999")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
