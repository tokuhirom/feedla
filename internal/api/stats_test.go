package api_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

// TestStatsInternalErrorsScopedToSubscriber covers a gap the Phase B
// user_id-scoping sweep missed: the crawler's internal-error ring buffer is
// process-wide (every user's crawls land in it), so /api/v1/stats must
// filter it down to the caller's own subscribed feeds -- otherwise any
// authenticated user could read internal error details about a feed only
// someone else subscribes to (docs/multi-user-design.md's feeds-sharing
// section: stats/error info is limited to "自分が購読している feed").
func TestStatsInternalErrorsScopedToSubscriber(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, testFeedXML)
	}))
	defer feedSrv.Close()

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

	apiSrv := httptest.NewServer(api.NewHandler(st, cr, fetcher, nil, api.Options{}))
	t.Cleanup(apiSrv.Close)
	owner := loginTestClient(t, apiSrv.URL)

	resp := subscribe(t, owner, apiSrv.URL, feedSrv.URL+"/feed")
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("subscribe status = %d, want 201: %s", resp.StatusCode, body)
	}
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)

	// Force an internal (feedla-side) crawl failure on the owner's feed by
	// closing the write DB out from under it -- same technique
	// TestCrawlerTreatsStoreWriteFailureAsInternal uses in the crawler
	// package. The fetch/parse still succeed; only the write that follows
	// fails, which is exactly what lands in the internal-error buffer.
	if err := st.Write.Close(); err != nil {
		t.Fatalf("Write.Close: %v", err)
	}
	if _, err := cr.CrawlAll(context.Background(), time.Now()); err != nil {
		t.Fatalf("CrawlAll: %v", err)
	}
	if entries := cr.RecentInternalErrors(); len(entries) != 1 || entries[0].FeedID != feedID {
		t.Fatalf("RecentInternalErrors = %+v, want one entry for feed %d", entries, feedID)
	}

	type statsBody struct {
		InternalErrors []struct {
			FeedID int64 `json:"feed_id"`
		} `json:"internal_errors"`
	}

	ownerResp, err := owner.Get(apiSrv.URL + "/api/v1/stats")
	if err != nil {
		t.Fatalf("GET stats (owner): %v", err)
	}
	var ownerStats statsBody
	decodeJSON(t, ownerResp, &ownerStats)
	if len(ownerStats.InternalErrors) != 1 || ownerStats.InternalErrors[0].FeedID != feedID {
		t.Fatalf("owner internal_errors = %+v, want one entry for feed %d", ownerStats.InternalErrors, feedID)
	}

	otherResp, err := other.Get(apiSrv.URL + "/api/v1/stats")
	if err != nil {
		t.Fatalf("GET stats (other): %v", err)
	}
	var otherStats statsBody
	decodeJSON(t, otherResp, &otherStats)
	if len(otherStats.InternalErrors) != 0 {
		t.Fatalf("other user's internal_errors = %+v, want empty (not subscribed to feed %d)", otherStats.InternalErrors, feedID)
	}
}
