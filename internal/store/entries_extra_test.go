package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestMarkAllEntriesRead(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()
	feedA, err := st.UpsertFeed(ctx, "https://example.com/feed-a", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed A: %v", err)
	}
	feedB, err := st.UpsertFeed(ctx, "https://example.com/feed-b", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed B: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedA, nil, "Feed A", now); err != nil {
		t.Fatalf("UpsertSubscription A: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedB, nil, "Feed B", now); err != nil {
		t.Fatalf("UpsertSubscription B: %v", err)
	}

	insertEntry(t, st, feedA, "a-1", now, now)
	insertEntry(t, st, feedA, "a-2", now, now)
	readID := insertEntry(t, st, feedB, "b-1", now, now)
	insertEntry(t, st, feedB, "b-2", now, now)

	// b-1 is already read before the bulk call, and shouldn't be counted
	// again nor break unread_count accounting for feed B.
	if _, err := st.MarkEntriesRead(ctx, testUserID, []int64{readID}, now); err != nil {
		t.Fatalf("MarkEntriesRead: %v", err)
	}

	n, err := st.MarkAllEntriesRead(ctx, testUserID, now)
	if err != nil {
		t.Fatalf("MarkAllEntriesRead: %v", err)
	}
	if n != 3 {
		t.Fatalf("marked_read = %d, want 3", n)
	}

	for _, feedID := range []int64{feedA, feedB} {
		view, err := st.GetSubscriptionView(ctx, testUserID, feedID)
		if err != nil {
			t.Fatalf("GetSubscriptionView(%d): %v", feedID, err)
		}
		if view.UnreadCount != 0 {
			t.Errorf("feed %d UnreadCount = %d, want 0", feedID, view.UnreadCount)
		}
	}

	// Calling it again with nothing left unread should be a no-op.
	n, err = st.MarkAllEntriesRead(ctx, testUserID, now)
	if err != nil {
		t.Fatalf("MarkAllEntriesRead (second call): %v", err)
	}
	if n != 0 {
		t.Fatalf("marked_read on second call = %d, want 0", n)
	}
}
