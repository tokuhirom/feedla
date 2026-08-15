package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

// todayTestFeedXML has one item published just now and one published well
// outside the 24h window, so tests can assert the Today endpoint/badge only
// picks up the recent one.
func todayTestFeedXML(now time.Time) string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Test Feed</title>
<link>https://example.com/</link>
<item>
  <title>Recent</title>
  <link>https://example.com/recent</link>
  <guid>guid-recent</guid>
  <pubDate>%s</pubDate>
  <description>Body recent</description>
</item>
<item>
  <title>Old</title>
  <link>https://example.com/old</link>
  <guid>guid-old</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body old</description>
</item>
</channel></rss>`, now.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))
}

func TestListTodayEntriesOnlyIncludesLast24h(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)
	now := time.Now()
	feedSrv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, todayTestFeedXML(now))
	})

	entries := subscribeAndFetchEntries(t, apiSrv, feedSrv.URL)
	if len(entries) != 2 {
		t.Fatalf("initial entries = %d, want 2", len(entries))
	}

	resp, err := http.Get(apiSrv.URL + "/api/v1/entries/today")
	if err != nil {
		t.Fatalf("GET /api/v1/entries/today: %v", err)
	}
	var todayResp struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, resp, &todayResp)
	if len(todayResp.Entries) != 1 || todayResp.Entries[0].GUID != "guid-recent" {
		t.Fatalf("today entries = %+v, want just guid-recent", todayResp.Entries)
	}

	// The subscriptions list badge should agree.
	subsResp, err := http.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET /api/v1/subscriptions: %v", err)
	}
	var subsList struct {
		TodayUnreadCount int64 `json:"today_unread_count"`
	}
	decodeJSON(t, subsResp, &subsList)
	if subsList.TodayUnreadCount != 1 {
		t.Fatalf("today_unread_count = %d, want 1", subsList.TodayUnreadCount)
	}

	// Marking the recent entry read should drop both the endpoint result
	// and the badge to zero.
	markResp := postJSON(t, apiSrv.URL+"/api/v1/entries/read",
		map[string]any{"entry_ids": []int64{todayResp.Entries[0].ID}})
	var markBody struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, markResp, &markBody)
	if markBody.MarkedRead != 1 {
		t.Fatalf("marked_read = %d, want 1", markBody.MarkedRead)
	}

	resp, err = http.Get(apiSrv.URL + "/api/v1/entries/today")
	if err != nil {
		t.Fatalf("GET /api/v1/entries/today after mark-read: %v", err)
	}
	decodeJSON(t, resp, &todayResp)
	if len(todayResp.Entries) != 0 {
		t.Fatalf("today entries after mark-read = %+v, want none", todayResp.Entries)
	}

	subsResp, err = http.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET /api/v1/subscriptions after mark-read: %v", err)
	}
	decodeJSON(t, subsResp, &subsList)
	if subsList.TodayUnreadCount != 0 {
		t.Fatalf("today_unread_count after mark-read = %d, want 0", subsList.TodayUnreadCount)
	}
}
