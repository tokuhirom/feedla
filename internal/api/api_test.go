package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
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

// Fixed test-only credentials used to complete the bootstrap admin's setup
// in every test server below (see loginTestClient).
const (
	testUsername = "test-admin"
	testPassword = "test-password-123456"
)

// newTestServerNoLogin wires up a real store + crawler (pointed at a local
// httptest feed server, with SSRF protection disabled so it can reach
// loopback) behind api.NewHandler, and returns an httptest.Server for it
// with setup still pending -- see newTestServer for the common case that
// also logs in.
func newTestServerNoLogin(t *testing.T) (apiSrv *httptest.Server, feedSrv *httptest.Server) {
	t.Helper()
	return newTestServerNoLoginWithOptions(t, api.Options{})
}

// newTestServerNoLoginWithOptions is newTestServerNoLogin but lets the
// caller override api.Options -- e.g. to set a small Quota so quota-limit
// tests don't need thousands of requests to hit a limit.
func newTestServerNoLoginWithOptions(t *testing.T, opts api.Options) (apiSrv *httptest.Server, feedSrv *httptest.Server) {
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
	return apiSrv, feedSrv
}

// newTestServer is newTestServerNoLogin plus an *http.Client already logged
// in as the (test-only) admin user -- every protected endpoint requires
// this now that auth is mandatory.
func newTestServer(t *testing.T) (apiSrv *httptest.Server, feedSrv *httptest.Server, client *http.Client) {
	t.Helper()
	apiSrv, feedSrv = newTestServerNoLogin(t)
	client = loginTestClient(t, apiSrv.URL)
	return apiSrv, feedSrv, client
}

// newTestServerWithOptions is newTestServer but lets the caller override
// api.Options (see newTestServerNoLoginWithOptions).
func newTestServerWithOptions(t *testing.T, opts api.Options) (apiSrv *httptest.Server, feedSrv *httptest.Server, client *http.Client) {
	t.Helper()
	apiSrv, feedSrv = newTestServerNoLoginWithOptions(t, opts)
	client = loginTestClient(t, apiSrv.URL)
	return apiSrv, feedSrv, client
}

// loginTestClient completes the bootstrap admin's initial setup (a fresh
// DB always has it pending) and returns an *http.Client whose cookie jar
// holds the resulting session -- every subsequent request through it is
// authenticated the same way a logged-in browser would be.
func loginTestClient(t *testing.T, apiSrvURL string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	resp := postJSON(t, client, apiSrvURL+"/api/v1/auth/setup", map[string]string{
		"username": testUsername,
		"password": testPassword,
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("test setup login status = %d, want 200: %s", resp.StatusCode, body)
	}
	return client
}

// origin returns the scheme://host of urlStr, as sent in the Origin header
// -- state-changing, cookie-authenticated requests need it to pass the
// CSRF check in internal/api/auth_middleware.go.
func origin(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// doWithOrigin builds and executes an authenticated, CSRF-safe request:
// the client supplies the session cookie, and the Origin header is set to
// urlStr's own origin so the middleware's same-origin check passes.
func doWithOrigin(t *testing.T, client *http.Client, method, urlStr, contentType string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, urlStr, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Origin", origin(urlStr))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, urlStr, err)
	}
	return resp
}

func postJSON(t *testing.T, client *http.Client, urlStr string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	return doWithOrigin(t, client, http.MethodPost, urlStr, "application/json", &buf)
}

func postForm(t *testing.T, client *http.Client, urlStr string, form url.Values) *http.Response {
	t.Helper()
	return doWithOrigin(t, client, http.MethodPost, urlStr, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
}

func deleteReq(t *testing.T, client *http.Client, urlStr string) *http.Response {
	t.Helper()
	return doWithOrigin(t, client, http.MethodDelete, urlStr, "", nil)
}

// subscribe drives POST /api/v1/subscriptions the way AddSubscriptionDialog
// does: an initial discovery call (always 202 + candidates, even for a
// single discovered feed -- see handleCreateSubscription) followed by a
// confirmed call for whichever candidate the caller wants. Most tests just
// want "subscribed successfully" and don't care about the candidate UX
// itself, so this returns the final create response for them to assert on
// like the old one-shot POST used to return.
func subscribe(t *testing.T, client *http.Client, apiSrvURL, feedURL string) *http.Response {
	t.Helper()
	return subscribeWithFulltext(t, client, apiSrvURL, feedURL, false)
}

func subscribeWithFulltext(t *testing.T, client *http.Client, apiSrvURL, feedURL string, fulltext bool) *http.Response {
	t.Helper()
	discover := postJSON(t, client, apiSrvURL+"/api/v1/subscriptions", map[string]string{"url": feedURL})
	if discover.StatusCode != http.StatusAccepted {
		t.Fatalf("discover subscribe status = %d, want 202", discover.StatusCode)
	}
	var discovered struct {
		Candidates []struct {
			Title    string `json:"title"`
			FeedURL  string `json:"feed_url"`
			Fulltext bool   `json:"fulltext"`
		} `json:"candidates"`
	}
	decodeJSON(t, discover, &discovered)

	var chosenURL string
	found := false
	for _, c := range discovered.Candidates {
		if c.Fulltext == fulltext {
			chosenURL = c.FeedURL
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no candidate with fulltext=%v in %+v", fulltext, discovered.Candidates)
	}

	return postJSON(t, client, apiSrvURL+"/api/v1/subscriptions", map[string]any{
		"url": chosenURL, "confirmed": true, "fulltext": fulltext,
	})
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestSubscribeFetchUnreadAndMarkRead(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)

	// Subscribe.
	resp := subscribe(t, client, apiSrv.URL, feedSrv.URL)
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
	resp, err := client.Get(apiSrv.URL + "/api/v1/subscriptions")
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
	resp, err = client.Get(entriesURL)
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
	resp = postJSON(t, client, apiSrv.URL+"/api/v1/entries/read", map[string]any{"entry_ids": ids})
	var markResp struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, resp, &markResp)
	if markResp.MarkedRead != 2 {
		t.Fatalf("marked_read = %d, want 2", markResp.MarkedRead)
	}

	// Unread entries should now be empty, and the subscription's
	// unread_count should reflect it.
	resp, err = client.Get(entriesURL)
	if err != nil {
		t.Fatalf("GET entries after read: %v", err)
	}
	decodeJSON(t, resp, &entriesResp)
	if len(entriesResp.Entries) != 0 {
		t.Fatalf("unread entries after mark-read = %d, want 0", len(entriesResp.Entries))
	}

	resp, err = client.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions after read: %v", err)
	}
	decodeJSON(t, resp, &list)
	if list.Subscriptions[0].UnreadCount != 0 {
		t.Errorf("UnreadCount after mark-read = %d, want 0", list.Subscriptions[0].UnreadCount)
	}
}

func TestMarkAllEntriesReadAPI(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)

	// A second, distinct feed so the bulk endpoint's cross-feed behavior
	// (unread_count refreshed for every touched feed, not just one) is
	// actually exercised.
	feedSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, strings.ReplaceAll(testFeedXML, "example.com", "example.org"))
	}))
	t.Cleanup(feedSrv2.Close)

	resp := subscribe(t, client, apiSrv.URL, feedSrv.URL)
	var created1 struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created1)

	resp = subscribe(t, client, apiSrv.URL, feedSrv2.URL)
	var created2 struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created2)

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/entries/read_all", nil)
	var markResp struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, resp, &markResp)
	if markResp.MarkedRead != 4 {
		t.Fatalf("marked_read = %d, want 4", markResp.MarkedRead)
	}

	resp, err := client.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions: %v", err)
	}
	var list struct {
		Subscriptions []store.SubscriptionView `json:"subscriptions"`
	}
	decodeJSON(t, resp, &list)
	for _, sub := range list.Subscriptions {
		if sub.UnreadCount != 0 {
			t.Errorf("feed %d UnreadCount = %d, want 0", sub.FeedID, sub.UnreadCount)
		}
	}
}

func TestDeleteSubscriptionRemovesFeed(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)

	resp := subscribe(t, client, apiSrv.URL, feedSrv.URL)
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	resp = deleteReq(t, client, fmt.Sprintf("%s/api/v1/subscriptions/%d", apiSrv.URL, feedID))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	resp, err := client.Get(apiSrv.URL + "/api/v1/subscriptions")
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
	apiSrv, feedSrv, client := newTestServer(t)

	resp := postForm(t, client, apiSrv.URL+"/api/subscribe", url.Values{"feedlink": {feedSrv.URL}})
	var subResp struct {
		SubscribeID int64 `json:"subscribe_id"`
	}
	decodeJSON(t, resp, &subResp)
	if subResp.SubscribeID == 0 {
		t.Fatal("subscribe_id = 0, want a real feed id")
	}

	resp = postForm(t, client, apiSrv.URL+"/api/unread", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	var entries []store.Entry
	decodeJSON(t, resp, &entries)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	resp = postForm(t, client, apiSrv.URL+"/api/touch_all", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	var touchResp struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, resp, &touchResp)
	if touchResp.MarkedRead != 2 {
		t.Fatalf("marked_read = %d, want 2", touchResp.MarkedRead)
	}

	resp = postForm(t, client, apiSrv.URL+"/api/unread", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	decodeJSON(t, resp, &entries)
	if len(entries) != 0 {
		t.Fatalf("len(entries) after touch_all = %d, want 0", len(entries))
	}
}

func TestCreateSubscriptionMissingURL(t *testing.T) {
	apiSrv, _, client := newTestServer(t)
	resp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestCreateSubscriptionAlwaysOffersCandidates covers handleCreateSubscription's
// UI-facing contract: even a URL that resolves to exactly one feed never
// auto-subscribes -- it always comes back as a 202 candidate list, with a
// synthetic fulltext-enabled variant of the first real candidate appended
// (see internal/fulltext, unrelated to feedless/pagewatch).
func TestCreateSubscriptionAlwaysOffersCandidates(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	if resp.StatusCode != http.StatusAccepted {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 202: %s", resp.StatusCode, body)
	}
	var discovered struct {
		Candidates []struct {
			Title    string `json:"title"`
			FeedURL  string `json:"feed_url"`
			Fulltext bool   `json:"fulltext"`
		} `json:"candidates"`
	}
	decodeJSON(t, resp, &discovered)
	if len(discovered.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want exactly 2 (the real feed + its fulltext variant)", discovered.Candidates)
	}
	if discovered.Candidates[0].Fulltext {
		t.Errorf("candidates[0].Fulltext = true, want the real candidate first")
	}
	if !discovered.Candidates[1].Fulltext || discovered.Candidates[1].FeedURL != discovered.Candidates[0].FeedURL {
		t.Errorf("candidates[1] = %+v, want a fulltext variant of candidates[0]", discovered.Candidates[1])
	}

	// Confirming the fulltext candidate creates a subscription with
	// Fulltext already set, before the first crawl even runs.
	confirmResp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]any{
		"url": discovered.Candidates[1].FeedURL, "confirmed": true, "fulltext": true,
	})
	if confirmResp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(confirmResp)
		t.Fatalf("confirm status = %d, want 201: %s", confirmResp.StatusCode, body)
	}
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, confirmResp, &created)
	if created.Subscription == nil || !created.Subscription.Fulltext {
		t.Fatalf("subscription = %+v, want Fulltext=true", created.Subscription)
	}
}

// TestCreateSubscriptionConfirmedKeepsDiscoveredTitleOnCrawlFailure covers a
// regression: the confirmed step skips feed.DiscoverFeed entirely (the
// caller already resolved the candidate), so it must carry the candidate's
// title forward to feed creation itself -- otherwise a feed whose very
// first crawl fails (down right after subscribing) ends up with no title
// at all, since crawlOne only overwrites feeds.title on a successful crawl.
func TestCreateSubscriptionConfirmedKeepsDiscoveredTitleOnCrawlFailure(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests > 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, testFeedXML)
	}))
	t.Cleanup(srv.Close)

	apiSrv, _, client := newTestServer(t)

	discoverResp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": srv.URL})
	if discoverResp.StatusCode != http.StatusAccepted {
		body, _ := decodeBody(discoverResp)
		t.Fatalf("discover status = %d, want 202: %s", discoverResp.StatusCode, body)
	}
	var discovered struct {
		Candidates []struct {
			Title    string `json:"title"`
			FeedURL  string `json:"feed_url"`
			Fulltext bool   `json:"fulltext"`
		} `json:"candidates"`
	}
	decodeJSON(t, discoverResp, &discovered)
	if discovered.Candidates[0].Title != "Test Feed" {
		t.Fatalf("candidates[0].Title = %q, want %q", discovered.Candidates[0].Title, "Test Feed")
	}

	// The confirm call's crawl (its own fetch of the same URL) is this
	// server's second request, which 404s -- feed creation must still
	// carry the title discovered above.
	confirmResp := postJSON(t, client, apiSrv.URL+"/api/v1/subscriptions", map[string]any{
		"url": discovered.Candidates[0].FeedURL, "confirmed": true, "title": discovered.Candidates[0].Title,
	})
	if confirmResp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(confirmResp)
		t.Fatalf("confirm status = %d, want 201: %s", confirmResp.StatusCode, body)
	}
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, confirmResp, &created)
	if created.Subscription == nil || created.Subscription.Title != "Test Feed" {
		t.Fatalf("subscription = %+v, want Title=%q", created.Subscription, "Test Feed")
	}
}

func TestHealthz(t *testing.T) {
	apiSrv, _, _ := newTestServer(t)
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
