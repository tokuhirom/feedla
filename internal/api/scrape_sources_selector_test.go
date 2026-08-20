package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/tokuhirom/feedla/internal/store"
)

const selectorListingHTML = `<html><head><title>お知らせ一覧</title></head><body>
<article><a href="/news/1">最初のお知らせ</a></article>
<article><a href="/news/2">2件目のお知らせ</a></article>
</body></html>`

func TestCreateScrapeSourceSelectorKind(t *testing.T) {
	apiSrv, pageSrv, _, client, _ := newTestServerWithPage(t, selectorListingHTML)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]any{
		"kind": "selector",
		"url":  pageSrv.URL,
		"config": map[string]any{
			"item_selector": "article",
			"fulltext":      false,
		},
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("create status = %d, want 201: %s", resp.StatusCode, body)
	}
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	if created.Subscription.Kind != "selector" {
		t.Errorf("Kind = %q, want selector", created.Subscription.Kind)
	}
	if created.Subscription.FeedURL != pageSrv.URL {
		t.Errorf("FeedURL = %q, want %q (selector: prefix stripped)", created.Subscription.FeedURL, pageSrv.URL)
	}
	if created.Subscription.UnreadCount != 2 {
		t.Errorf("UnreadCount = %d, want 2 (both listed articles, fulltext disabled)", created.Subscription.UnreadCount)
	}

	resp2, err := client.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions: %v", err)
	}
	var list struct {
		Subscriptions []store.SubscriptionView `json:"subscriptions"`
	}
	decodeJSON(t, resp2, &list)
	if len(list.Subscriptions) != 1 || list.Subscriptions[0].Kind != "selector" {
		t.Fatalf("subscriptions = %+v, want one selector subscription", list.Subscriptions)
	}
}

func TestCreateScrapeSourceSelectorMissingItemSelectorRejected(t *testing.T) {
	apiSrv, pageSrv, _, client, _ := newTestServerWithPage(t, selectorListingHTML)
	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]any{
		"kind":   "selector",
		"url":    pageSrv.URL,
		"config": map[string]any{},
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestCreateScrapeSourceUnsupportedKindRejected(t *testing.T) {
	apiSrv, pageSrv, _, client, _ := newTestServerWithPage(t, selectorListingHTML)
	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]any{
		"kind": "urlpattern",
		"url":  pageSrv.URL,
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestScrapeSourceSelectorGetPatchAndPreview(t *testing.T) {
	apiSrv, pageSrv, _, client, _ := newTestServerWithPage(t, selectorListingHTML)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources", map[string]any{
		"kind":   "selector",
		"url":    pageSrv.URL,
		"config": map[string]any{"item_selector": "article", "fulltext": false},
	})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	listResp, err := client.Get(apiSrv.URL + "/api/v1/scrape_sources")
	if err != nil {
		t.Fatalf("GET scrape_sources: %v", err)
	}
	var list struct {
		ScrapeSources []struct {
			ID     int64  `json:"id"`
			FeedID int64  `json:"feed_id"`
			Kind   string `json:"kind"`
		} `json:"scrape_sources"`
	}
	decodeJSON(t, listResp, &list)
	if len(list.ScrapeSources) != 1 || list.ScrapeSources[0].FeedID != feedID || list.ScrapeSources[0].Kind != "selector" {
		t.Fatalf("scrape_sources = %+v, want one selector entry for feed %d", list.ScrapeSources, feedID)
	}
	srcID := list.ScrapeSources[0].ID

	// PATCH with a selector-shaped config must be validated against
	// selector.ParseConfig, not pagewatch's -- before the kind-dispatch fix
	// this always ran pagewatch.ParseConfig regardless of the saved kind.
	patchResp := patchJSON(t, client, fmt.Sprintf("%s/api/v1/scrape_sources/%d", apiSrv.URL, srcID), map[string]any{
		"config": map[string]any{"item_selector": "article", "title_selector": "a", "fulltext": false},
	})
	if patchResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(patchResp)
		t.Fatalf("PATCH status = %d, want 200: %s", patchResp.StatusCode, body)
	}

	// A pagewatch-shaped config (missing item_selector) must be rejected for
	// a selector-kind source.
	badPatch := patchJSON(t, client, fmt.Sprintf("%s/api/v1/scrape_sources/%d", apiSrv.URL, srcID), map[string]any{
		"config": map[string]any{"ignore_patterns": []string{"x"}},
	})
	if badPatch.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(badPatch)
		t.Fatalf("PATCH with pagewatch-shaped config status = %d, want 400: %s", badPatch.StatusCode, body)
	}

	previewResp := postJSON(t, client, fmt.Sprintf("%s/api/v1/scrape_sources/%d/preview", apiSrv.URL, srcID), nil)
	if previewResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(previewResp)
		t.Fatalf("preview status = %d, want 200: %s", previewResp.StatusCode, body)
	}
	var preview struct {
		Items []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
			Seen  bool   `json:"seen"`
		} `json:"items"`
		Matched int `json:"matched"`
	}
	decodeJSON(t, previewResp, &preview)
	if preview.Matched != 2 {
		t.Fatalf("Matched = %d, want 2", preview.Matched)
	}
	if len(preview.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(preview.Items))
	}
	for _, item := range preview.Items {
		if !item.Seen {
			t.Errorf("item %+v: Seen = false, want true (already imported during initial crawl)", item)
		}
	}
}

// TestScrapeSourceSelectorPatchAndPreviewOwnershipEnforced mirrors
// TestScrapeSourcePatchAndPreviewOwnershipEnforced but for kind "selector",
// per CLAUDE.md's IDOR-test requirement for any authorization-bearing
// endpoint change (the PATCH/preview kind-dispatch added here is exactly
// such a change).
func TestScrapeSourceSelectorPatchAndPreviewOwnershipEnforced(t *testing.T) {
	apiSrv, pageSrv, _, owner, st := newTestServerWithPage(t, selectorListingHTML)

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/scrape_sources", map[string]any{
		"kind":   "selector",
		"url":    pageSrv.URL,
		"config": map[string]any{"item_selector": "article", "fulltext": false},
	})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	listResp, err := owner.Get(apiSrv.URL + "/api/v1/scrape_sources")
	if err != nil {
		t.Fatalf("GET scrape_sources: %v", err)
	}
	var list struct {
		ScrapeSources []struct {
			ID     int64 `json:"id"`
			FeedID int64 `json:"feed_id"`
		} `json:"scrape_sources"`
	}
	decodeJSON(t, listResp, &list)
	if len(list.ScrapeSources) != 1 || list.ScrapeSources[0].FeedID != feedID {
		t.Fatalf("scrape_sources = %+v, want one entry for feed %d", list.ScrapeSources, feedID)
	}
	srcID := list.ScrapeSources[0].ID

	patchURL := fmt.Sprintf("%s/api/v1/scrape_sources/%d", apiSrv.URL, srcID)
	previewURL := fmt.Sprintf("%s/api/v1/scrape_sources/%d/preview", apiSrv.URL, srcID)
	patchBody := map[string]any{"config": map[string]any{"item_selector": "article", "fulltext": false}}

	other := createTestUser(t, st, apiSrv.URL, "other-user-selector", false)
	if resp := patchJSON(t, other, patchURL, patchBody); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-owner PATCH status = %d, want 404: %s", resp.StatusCode, body)
	}
	if resp := postJSON(t, other, previewURL, nil); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-owner preview status = %d, want 404: %s", resp.StatusCode, body)
	}

	admin := createTestUser(t, st, apiSrv.URL, "other-admin-selector", true)
	if resp := patchJSON(t, admin, patchURL, patchBody); resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("admin PATCH status = %d, want 200: %s", resp.StatusCode, body)
	}
	if resp := postJSON(t, admin, previewURL, nil); resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("admin preview status = %d, want 200: %s", resp.StatusCode, body)
	}
}

func TestPreviewUnsavedScrapeSourceSelector(t *testing.T) {
	apiSrv, pageSrv, _, client, _ := newTestServerWithPage(t, selectorListingHTML)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources/preview", map[string]any{
		"kind":   "selector",
		"url":    pageSrv.URL,
		"config": map[string]any{"item_selector": "article"},
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	var preview struct {
		Items   []json.RawMessage `json:"items"`
		Matched int               `json:"matched"`
	}
	decodeJSON(t, resp, &preview)
	if preview.Matched != 2 || len(preview.Items) != 2 {
		t.Fatalf("preview = %+v, want 2 matched items", preview)
	}

	// No scrape source was actually created.
	listResp, err := client.Get(apiSrv.URL + "/api/v1/scrape_sources")
	if err != nil {
		t.Fatalf("GET scrape_sources: %v", err)
	}
	var list struct {
		ScrapeSources []json.RawMessage `json:"scrape_sources"`
	}
	decodeJSON(t, listResp, &list)
	if len(list.ScrapeSources) != 0 {
		t.Fatalf("scrape_sources = %+v, want none (preview must not persist anything)", list.ScrapeSources)
	}
}

func TestPreviewUnsavedScrapeSourceInvalidConfigRejected(t *testing.T) {
	apiSrv, pageSrv, _, client, _ := newTestServerWithPage(t, selectorListingHTML)
	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources/preview", map[string]any{
		"kind":   "selector",
		"url":    pageSrv.URL,
		"config": map[string]any{"item_selector": "["}, // invalid CSS selector
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
}

// TestPreviewUnsavedScrapeSourceRequiresAuth checks the "authentication is
// the only guard besides previewLimiter" property §8.2 relies on for this
// endpoint (it takes no id, so no ownership check is even possible).
func TestPreviewUnsavedScrapeSourceRequiresAuth(t *testing.T) {
	apiSrv, pageSrv, _, _, _ := newTestServerWithPage(t, selectorListingHTML)
	resp := postJSON(t, http.DefaultClient, apiSrv.URL+"/api/v1/scrape_sources/preview", map[string]any{
		"kind":   "selector",
		"url":    pageSrv.URL,
		"config": map[string]any{"item_selector": "article"},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := decodeBody(resp)
		t.Fatalf("unauthenticated status = %d, want 401: %s", resp.StatusCode, body)
	}
}
