package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestIgnoreWordsAddListRemove(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)
	entries := subscribeAndFetchEntries(t, client, apiSrv, feedSrv.URL)
	if len(entries) != 2 {
		t.Fatalf("initial entries = %d, want 2", len(entries))
	}

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/ignore_words", map[string]string{"word": "Item 1"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST ignore_words status = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	listResp, err := client.Get(apiSrv.URL + "/api/v1/ignore_words")
	if err != nil {
		t.Fatalf("GET ignore_words: %v", err)
	}
	var listed struct {
		IgnoreWords []store.IgnoreWord `json:"ignore_words"`
	}
	decodeJSON(t, listResp, &listed)
	if len(listed.IgnoreWords) != 1 || listed.IgnoreWords[0].Word != "Item 1" {
		t.Fatalf("ignore_words = %+v, want one 'Item 1' entry", listed.IgnoreWords)
	}

	entriesResp, err := client.Get(fmt.Sprintf("%s/api/v1/subscriptions/%d/entries", apiSrv.URL, entries[0].FeedID))
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	var afterAdd struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, entriesResp, &afterAdd)
	if len(afterAdd.Entries) != 1 || afterAdd.Entries[0].Title != "Item 2" {
		t.Fatalf("entries after ignore word = %+v, want only 'Item 2'", afterAdd.Entries)
	}

	delResp := deleteReq(t, client, fmt.Sprintf("%s/api/v1/ignore_words/%d", apiSrv.URL, listed.IgnoreWords[0].ID))
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE ignore_words status = %d, want 204", delResp.StatusCode)
	}
	_ = delResp.Body.Close()

	entriesResp, err = client.Get(fmt.Sprintf("%s/api/v1/subscriptions/%d/entries", apiSrv.URL, entries[0].FeedID))
	if err != nil {
		t.Fatalf("GET entries (after remove): %v", err)
	}
	var afterRemove struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, entriesResp, &afterRemove)
	if len(afterRemove.Entries) != 2 {
		t.Fatalf("entries after removing ignore word = %d, want 2", len(afterRemove.Entries))
	}
}

func TestAddIgnoreWordRequiresWord(t *testing.T) {
	apiSrv, _, client := newTestServer(t)
	resp := postJSON(t, client, apiSrv.URL+"/api/v1/ignore_words", map[string]string{"word": ""})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestRemoveIgnoreWordUnknown(t *testing.T) {
	apiSrv, _, client := newTestServer(t)
	resp := deleteReq(t, client, apiSrv.URL+"/api/v1/ignore_words/999999")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
