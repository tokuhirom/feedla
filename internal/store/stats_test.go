package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestGetStats(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	okFeedID, err := st.UpsertFeed(ctx, "https://ok.example.com/feed", "", "", 1800, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("UpsertFeed(ok): %v", err)
	}
	if err := st.UpsertSubscription(ctx, okFeedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription(ok): %v", err)
	}
	if _, err := st.UpsertEntries(ctx, okFeedID, []store.EntryInput{{
		GUID: "g1", URL: "https://ok.example.com/1", Title: "t", Body: "b",
		BodyHash: []byte("h1"), PublishedAt: now.Unix(), UpdatedAt: now.Unix(),
	}}, now); err != nil {
		t.Fatalf("UpsertEntries: %v", err)
	}

	failFeedID, err := st.UpsertFeed(ctx, "https://fail.example.com/feed", "", "", 1800, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("UpsertFeed(fail): %v", err)
	}
	if err := st.UpsertSubscription(ctx, failFeedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription(fail): %v", err)
	}
	msg := "boom"
	if err := st.UpdateFeedAfterFetch(ctx, failFeedID, store.FeedFetchUpdate{
		FetchIntervalSec: 1800,
		NextFetchAt:      now.Add(time.Hour).Unix(),
		LastStatus:       500,
		Success:          false,
		LastError:        &msg,
	}, now); err != nil {
		t.Fatalf("UpdateFeedAfterFetch: %v", err)
	}

	stats, err := st.GetStats(ctx, now)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if stats.FeedsTotal != 2 {
		t.Errorf("FeedsTotal = %d, want 2", stats.FeedsTotal)
	}
	if stats.FeedsErroring != 1 {
		t.Errorf("FeedsErroring = %d, want 1", stats.FeedsErroring)
	}
	if stats.EntriesUnread != 1 {
		t.Errorf("EntriesUnread = %d, want 1", stats.EntriesUnread)
	}
	// Only okFeedID's next_fetch_at (now-1h) is due; failFeedID's is 1h out.
	if stats.QueueDepth != 1 {
		t.Errorf("QueueDepth = %d, want 1", stats.QueueDepth)
	}
	if stats.DBSizeBytes <= 0 {
		t.Errorf("DBSizeBytes = %d, want > 0", stats.DBSizeBytes)
	}
	if len(stats.ErroringFeeds) != 1 || stats.ErroringFeeds[0].FeedID != failFeedID {
		t.Errorf("ErroringFeeds = %+v, want just failFeedID", stats.ErroringFeeds)
	}
}
