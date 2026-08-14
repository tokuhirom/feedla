package api_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/tokuhirom/feedla/internal/store"
)

// subscribeAndFetchEntries subscribes to feedSrv and returns the unread
// entries the initial crawl produced (mirroring TestSubscribeFetchUnreadAndMarkRead).
func subscribeAndFetchEntries(t *testing.T, apiSrv *httptest.Server, feedURL string) []store.Entry {
	t.Helper()
	resp := postJSON(t, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedURL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	if created.Subscription == nil {
		t.Fatal("subscribe: want a subscription in the response")
	}

	entriesURL := fmt.Sprintf("%s/api/v1/subscriptions/%d/entries", apiSrv.URL, created.Subscription.FeedID)
	resp, err := http.Get(entriesURL)
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	var entriesResp struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, resp, &entriesResp)
	return entriesResp.Entries
}

func TestSearchFindsEntriesByTitle(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)
	subscribeAndFetchEntries(t, apiSrv, feedSrv.URL)

	resp, err := http.Get(apiSrv.URL + "/api/v1/search?q=Item")
	if err != nil {
		t.Fatalf("GET search: %v", err)
	}
	var searchResp struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, resp, &searchResp)
	if len(searchResp.Entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(searchResp.Entries))
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	apiSrv, _ := newTestServer(t)
	resp, err := http.Get(apiSrv.URL + "/api/v1/search")
	if err != nil {
		t.Fatalf("GET search: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPinAddListRemove(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)
	entries := subscribeAndFetchEntries(t, apiSrv, feedSrv.URL)
	if len(entries) == 0 {
		t.Fatal("want at least one entry")
	}
	entryID := entries[0].ID

	resp := postJSON(t, apiSrv.URL+"/api/v1/pins", map[string]any{"entry_id": entryID})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add pin status = %d, want 201", resp.StatusCode)
	}

	resp, err := http.Get(apiSrv.URL + "/api/v1/pins")
	if err != nil {
		t.Fatalf("GET pins: %v", err)
	}
	var pinsResp struct {
		Pins []store.Pin `json:"pins"`
	}
	decodeJSON(t, resp, &pinsResp)
	if len(pinsResp.Pins) != 1 || pinsResp.Pins[0].EntryID != entryID {
		t.Fatalf("pins = %+v, want one pin for entry %d", pinsResp.Pins, entryID)
	}

	entriesURL := fmt.Sprintf("%s/api/v1/subscriptions/%d/entries", apiSrv.URL, entries[0].FeedID)
	resp, err = http.Get(entriesURL)
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	var entriesResp struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, resp, &entriesResp)
	found := false
	for _, e := range entriesResp.Entries {
		if e.ID == entryID {
			found = true
			if !e.Pinned {
				t.Errorf("entry %d Pinned = false, want true", entryID)
			}
		}
	}
	if !found {
		t.Fatalf("entry %d not found in entries list", entryID)
	}

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/pins/%d", apiSrv.URL, entryID), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE pin: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete pin status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(apiSrv.URL + "/api/v1/pins")
	if err != nil {
		t.Fatalf("GET pins after delete: %v", err)
	}
	decodeJSON(t, resp, &pinsResp)
	if len(pinsResp.Pins) != 0 {
		t.Fatalf("pins after delete = %+v, want empty", pinsResp.Pins)
	}
}

func TestLDRCompatPinAddAllRemove(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)
	entries := subscribeAndFetchEntries(t, apiSrv, feedSrv.URL)
	if len(entries) == 0 {
		t.Fatal("want at least one entry")
	}
	link := entries[0].URL

	resp, err := http.PostForm(apiSrv.URL+"/api/pin/add", url.Values{"link": {link}})
	if err != nil {
		t.Fatalf("POST /api/pin/add: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pin/add status = %d, want 200", resp.StatusCode)
	}

	resp, err = http.PostForm(apiSrv.URL+"/api/pin/all", nil)
	if err != nil {
		t.Fatalf("POST /api/pin/all: %v", err)
	}
	var pins []store.Pin
	decodeJSON(t, resp, &pins)
	if len(pins) != 1 || pins[0].URL != link {
		t.Fatalf("pins = %+v, want one pin for %q", pins, link)
	}

	resp, err = http.PostForm(apiSrv.URL+"/api/pin/remove", url.Values{"link": {link}})
	if err != nil {
		t.Fatalf("POST /api/pin/remove: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pin/remove status = %d, want 200", resp.StatusCode)
	}

	resp, err = http.PostForm(apiSrv.URL+"/api/pin/all", nil)
	if err != nil {
		t.Fatalf("POST /api/pin/all: %v", err)
	}
	decodeJSON(t, resp, &pins)
	if len(pins) != 0 {
		t.Fatalf("pins after remove = %+v, want empty", pins)
	}
}

func TestOPMLExportImportRoundTrip(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)
	subscribeAndFetchEntries(t, apiSrv, feedSrv.URL)

	resp, err := http.Get(apiSrv.URL + "/api/v1/opml")
	if err != nil {
		t.Fatalf("GET opml: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/x-opml; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/x-opml", ct)
	}
	body, err := decodeBody(resp)
	if err != nil {
		t.Fatalf("read export body: %v", err)
	}

	apiSrv2, _ := newTestServer(t)
	importResp, err := http.Post(apiSrv2.URL+"/api/v1/opml", "text/x-opml", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST opml import: %v", err)
	}
	if importResp.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d, want 200", importResp.StatusCode)
	}
	var importResult struct {
		Imported int `json:"imported"`
	}
	decodeJSON(t, importResp, &importResult)
	if importResult.Imported != 1 {
		t.Fatalf("imported = %d, want 1", importResult.Imported)
	}
}
