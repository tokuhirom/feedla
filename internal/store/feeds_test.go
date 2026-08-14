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

	if _, err := st.UpsertFeed(ctx, "https://a.example.com/feed", "", "", 1, now.Add(-time.Hour)); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if _, err := st.UpsertFeed(ctx, "https://b.example.com/feed", "", "", 1800, now.Add(time.Hour)); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
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
