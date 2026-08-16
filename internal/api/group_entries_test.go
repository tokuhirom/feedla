package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/tokuhirom/feedla/internal/store"
)

func patchJSON(t *testing.T, client *http.Client, urlStr string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return doWithOrigin(t, client, http.MethodPatch, urlStr, "application/json", &buf)
}

func TestListGroupEntriesByFolder(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)
	entries := subscribeAndFetchEntries(t, client, apiSrv, feedSrv.URL)
	if len(entries) != 2 {
		t.Fatalf("initial entries = %d, want 2", len(entries))
	}
	feedID := entries[0].FeedID

	folderResp := postJSON(t, client, apiSrv.URL+"/api/v1/folders", map[string]string{"name": "Tech"})
	var folder store.Folder
	decodeJSON(t, folderResp, &folder)

	resp := patchJSON(t, client, fmt.Sprintf("%s/api/v1/subscriptions/%d", apiSrv.URL, feedID),
		map[string]any{"folder_id": folder.ID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH subscription status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	groupResp, err := client.Get(fmt.Sprintf("%s/api/v1/entries?folder_id=%d", apiSrv.URL, folder.ID))
	if err != nil {
		t.Fatalf("GET group entries: %v", err)
	}
	var groupEntries struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, groupResp, &groupEntries)
	if len(groupEntries.Entries) != 2 {
		t.Fatalf("entries in folder = %+v, want 2", groupEntries.Entries)
	}

	unfiledResp, err := client.Get(apiSrv.URL + "/api/v1/entries?folder_id=0")
	if err != nil {
		t.Fatalf("GET unfiled group entries: %v", err)
	}
	var unfiled struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, unfiledResp, &unfiled)
	if len(unfiled.Entries) != 0 {
		t.Fatalf("unfiled entries = %+v, want none (the only feed is now in Tech)", unfiled.Entries)
	}
}

func TestListGroupEntriesByRating(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)
	entries := subscribeAndFetchEntries(t, client, apiSrv, feedSrv.URL)
	feedID := entries[0].FeedID

	resp := patchJSON(t, client, fmt.Sprintf("%s/api/v1/subscriptions/%d", apiSrv.URL, feedID),
		map[string]any{"rating": 4})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH subscription status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	groupResp, err := client.Get(apiSrv.URL + "/api/v1/entries?rating=4")
	if err != nil {
		t.Fatalf("GET group entries: %v", err)
	}
	var groupEntries struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, groupResp, &groupEntries)
	if len(groupEntries.Entries) != 2 {
		t.Fatalf("entries at rating 4 = %+v, want 2", groupEntries.Entries)
	}

	emptyResp, err := client.Get(apiSrv.URL + "/api/v1/entries?rating=5")
	if err != nil {
		t.Fatalf("GET group entries (rating 5): %v", err)
	}
	var empty struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, emptyResp, &empty)
	if len(empty.Entries) != 0 {
		t.Fatalf("entries at rating 5 = %+v, want none", empty.Entries)
	}
}

func TestListGroupEntriesRequiresExactlyOneFilter(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	resp, err := client.Get(apiSrv.URL + "/api/v1/entries")
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status (no filter) = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp, err = client.Get(apiSrv.URL + "/api/v1/entries?folder_id=0&rating=1")
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status (both filters) = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
