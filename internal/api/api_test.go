package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

const testFeedXML = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Test Feed</title>
<link>https://example.com/</link>
<item>
  <title>Item 1</title>
  <link>https://example.com/1</link>
  <guid>guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body 1</description>
</item>
<item>
  <title>Item 2</title>
  <link>https://example.com/2</link>
  <guid>guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body 2</description>
</item>
</channel></rss>`

// newTestServer wires up a real store + crawler (pointed at a local
// httptest feed server, with SSRF protection disabled so it can reach
// loopback) behind api.NewHandler, and returns an httptest.Server for it.
func newTestServer(t *testing.T) (apiSrv *httptest.Server, feedSrv *httptest.Server) {
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

	apiSrv = httptest.NewServer(api.NewHandler(st, cr, fetcher))
	t.Cleanup(apiSrv.Close)
	return apiSrv, feedSrv
}

func postJSON(t *testing.T, urlStr string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	resp, err := http.Post(urlStr, "application/json", &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", urlStr, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestSubscribeFetchUnreadAndMarkRead(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)

	// Subscribe.
	resp := postJSON(t, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("subscribe status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	if created.Subscription == nil {
		t.Fatal("subscribe: want a subscription in the response")
	}
	feedID := created.Subscription.FeedID
	if created.Subscription.UnreadCount != 2 {
		t.Errorf("UnreadCount = %d, want 2 (subscribe crawls immediately)", created.Subscription.UnreadCount)
	}

	// List subscriptions.
	resp, err := http.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions: %v", err)
	}
	var list struct {
		Subscriptions []store.SubscriptionView `json:"subscriptions"`
	}
	decodeJSON(t, resp, &list)
	if len(list.Subscriptions) != 1 {
		t.Fatalf("len(subscriptions) = %d, want 1", len(list.Subscriptions))
	}

	// Fetch unread entries.
	entriesURL := fmt.Sprintf("%s/api/v1/subscriptions/%d/entries?unread=1", apiSrv.URL, feedID)
	resp, err = http.Get(entriesURL)
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

	// Mark them read.
	ids := []int64{entriesResp.Entries[0].ID, entriesResp.Entries[1].ID}
	resp = postJSON(t, apiSrv.URL+"/api/v1/entries/read", map[string]any{"entry_ids": ids})
	var markResp struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, resp, &markResp)
	if markResp.MarkedRead != 2 {
		t.Fatalf("marked_read = %d, want 2", markResp.MarkedRead)
	}

	// Unread entries should now be empty, and the subscription's
	// unread_count should reflect it.
	resp, err = http.Get(entriesURL)
	if err != nil {
		t.Fatalf("GET entries after read: %v", err)
	}
	decodeJSON(t, resp, &entriesResp)
	if len(entriesResp.Entries) != 0 {
		t.Fatalf("unread entries after mark-read = %d, want 0", len(entriesResp.Entries))
	}

	resp, err = http.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions after read: %v", err)
	}
	decodeJSON(t, resp, &list)
	if list.Subscriptions[0].UnreadCount != 0 {
		t.Errorf("UnreadCount after mark-read = %d, want 0", list.Subscriptions[0].UnreadCount)
	}
}

func TestDeleteSubscriptionRemovesFeed(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)

	resp := postJSON(t, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/subscriptions/%d", apiSrv.URL, feedID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions: %v", err)
	}
	var list struct {
		Subscriptions []store.SubscriptionView `json:"subscriptions"`
	}
	decodeJSON(t, resp, &list)
	if len(list.Subscriptions) != 0 {
		t.Fatalf("len(subscriptions) after delete = %d, want 0", len(list.Subscriptions))
	}
}

func TestLDRCompatSubscribeUnreadTouchAll(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)

	form := url.Values{"feedlink": {feedSrv.URL}}
	resp, err := http.PostForm(apiSrv.URL+"/api/subscribe", form)
	if err != nil {
		t.Fatalf("POST /api/subscribe: %v", err)
	}
	var subResp struct {
		SubscribeID int64 `json:"subscribe_id"`
	}
	decodeJSON(t, resp, &subResp)
	if subResp.SubscribeID == 0 {
		t.Fatal("subscribe_id = 0, want a real feed id")
	}

	resp, err = http.PostForm(apiSrv.URL+"/api/unread", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	if err != nil {
		t.Fatalf("POST /api/unread: %v", err)
	}
	var entries []store.Entry
	decodeJSON(t, resp, &entries)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	resp, err = http.PostForm(apiSrv.URL+"/api/touch_all", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	if err != nil {
		t.Fatalf("POST /api/touch_all: %v", err)
	}
	var touchResp struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, resp, &touchResp)
	if touchResp.MarkedRead != 2 {
		t.Fatalf("marked_read = %d, want 2", touchResp.MarkedRead)
	}

	resp, err = http.PostForm(apiSrv.URL+"/api/unread", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	if err != nil {
		t.Fatalf("POST /api/unread after touch_all: %v", err)
	}
	decodeJSON(t, resp, &entries)
	if len(entries) != 0 {
		t.Fatalf("len(entries) after touch_all = %d, want 0", len(entries))
	}
}

func TestCreateSubscriptionMissingURL(t *testing.T) {
	apiSrv, _ := newTestServer(t)
	resp := postJSON(t, apiSrv.URL+"/api/v1/subscriptions", map[string]string{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	apiSrv, _ := newTestServer(t)
	resp, err := http.Get(apiSrv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := decodeBody(resp)
	if !strings.Contains(body, `"ok"`) {
		t.Errorf("body = %q, want it to mention ok", body)
	}
}

func decodeBody(resp *http.Response) (string, error) {
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	return buf.String(), err
}
