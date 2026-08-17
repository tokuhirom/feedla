package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestClaimDueFeedsPreventsDoubleDispatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)

	feedA, err := st.UpsertFeed(ctx, "https://a.example.com/feed", "", "", 1, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	feedB, err := st.UpsertFeed(ctx, "https://b.example.com/feed", "", "", 1800, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	// ClaimDueFeeds only claims subscribed feeds (docs/multi-user-design.md's
	// GC section).
	if err := st.UpsertSubscription(ctx, testUserID, feedA, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription(a): %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedB, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription(b): %v", err)
	}

	// Only the feed already due (negative jitter can't happen, so force it
	// due by using a `now` far enough in the future) should be claimed.
	claimed, err := st.ClaimDueFeeds(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueFeeds: %v", err)
	}
	if len(claimed) != 1 || claimed[0].FeedURL != "https://a.example.com/feed" {
		t.Fatalf("claimed = %+v, want exactly the due feed", claimed)
	}

	// A second claim immediately after must return nothing: the first claim
	// already pushed next_fetch_at out by the feed's own interval.
	claimedAgain, err := st.ClaimDueFeeds(ctx, now, 10)
	if err != nil {
		t.Fatalf("second ClaimDueFeeds: %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("second claim = %+v, want empty (feed already claimed)", claimedAgain)
	}

	// Once enough simulated time passes (past the 1s interval), it's due again.
	claimedLater, err := st.ClaimDueFeeds(ctx, now.Add(2*time.Second), 10)
	if err != nil {
		t.Fatalf("later ClaimDueFeeds: %v", err)
	}
	if len(claimedLater) != 1 {
		t.Fatalf("later claim = %+v, want the feed to be due again", claimedLater)
	}
}

// TestClaimAndListDueFeedsExcludeUnsubscribed reproduces the "orphan feed
// crawled forever" gap docs/multi-user-design.md's GC section calls out:
// once every subscriber leaves, a due feed must stop showing up in either
// ClaimDueFeeds (the scheduler's path) or ListDueFeeds.
func TestClaimAndListDueFeedsExcludeUnsubscribed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)

	subscribed, err := st.UpsertFeed(ctx, "https://subscribed.example.com/feed", "", "", 1, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("UpsertFeed(subscribed): %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, subscribed, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	if _, err := st.UpsertFeed(ctx, "https://orphan.example.com/feed", "", "", 1, now.Add(-time.Hour)); err != nil {
		t.Fatalf("UpsertFeed(orphan): %v", err)
	}

	listed, err := st.ListDueFeeds(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListDueFeeds: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != subscribed {
		t.Fatalf("ListDueFeeds = %+v, want only the subscribed feed", listed)
	}

	claimed, err := st.ClaimDueFeeds(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueFeeds: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != subscribed {
		t.Fatalf("ClaimDueFeeds = %+v, want only the subscribed feed", claimed)
	}
}

func TestUpdateFeedAfterFetchGoneStopsFutureCrawls(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, "https://gone.example.com/feed", "", "", 1800, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	msg := "410 gone"
	err = st.UpdateFeedAfterFetch(ctx, feedID, store.FeedFetchUpdate{
		FetchIntervalSec: 1800,
		NextFetchAt:      now.Unix(),
		LastStatus:       410,
		Success:          false,
		Gone:             true,
		LastError:        &msg,
	}, now)
	if err != nil {
		t.Fatalf("UpdateFeedAfterFetch: %v", err)
	}

	due, err := st.ClaimDueFeeds(ctx, now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ClaimDueFeeds: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due = %+v, want the 410'd feed excluded (error_count >= 20)", due)
	}
}

// TestUpdateFeedAfterFetchSkipsFeedURLOnConflict reproduces the "constraint
// failed: UNIQUE constraint failed: feeds.feed_url" internal error seen in
// production: a feed's fetch permanently redirects to a URL that's already
// registered as a different feed. The update must succeed (not error forever
// on every future tick) by leaving feed_url unchanged.
func TestUpdateFeedAfterFetchSkipsFeedURLOnConflict(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	existingID, err := st.UpsertFeed(ctx, "https://existing.example.com/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed(existing): %v", err)
	}
	redirectedID, err := st.UpsertFeed(ctx, "https://redirected.example.com/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed(redirected): %v", err)
	}

	newURL := "https://existing.example.com/feed"
	err = st.UpdateFeedAfterFetch(ctx, redirectedID, store.FeedFetchUpdate{
		NewFeedURL:       &newURL,
		FetchIntervalSec: 1800,
		NextFetchAt:      now.Unix(),
		LastStatus:       200,
		Success:          true,
	}, now)
	if err != nil {
		t.Fatalf("UpdateFeedAfterFetch: %v, want feed_url conflict to be recovered instead of erroring", err)
	}

	got, err := st.GetFeed(ctx, redirectedID)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if got.FeedURL != "https://redirected.example.com/feed" {
		t.Fatalf("FeedURL = %q, want the original URL preserved (not overwritten with the conflicting one)", got.FeedURL)
	}
	if got.LastError == nil || *got.LastError == "" {
		t.Fatalf("LastError = %v, want a note about the skipped feed_url conflict", got.LastError)
	}

	other, err := st.GetFeed(ctx, existingID)
	if err != nil {
		t.Fatalf("GetFeed(existing): %v", err)
	}
	if other.FeedURL != "https://existing.example.com/feed" {
		t.Fatalf("existing feed's FeedURL = %q, must be untouched", other.FeedURL)
	}
}
