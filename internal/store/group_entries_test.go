package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

// newTestStoreWithFeeds creates n feeds (each with one entry) and subscribes
// to all of them, returning the store and their feed IDs in creation order.
func newTestStoreWithFeeds(t *testing.T, n int) (*store.Store, []int64) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	feedIDs := make([]int64, n)
	for i := range n {
		feedID, err := st.UpsertFeed(ctx, "https://example.com/feed"+string(rune('a'+i)), "", "", 1800, now)
		if err != nil {
			t.Fatalf("UpsertFeed: %v", err)
		}
		if err := st.UpsertSubscription(ctx, feedID, nil, "", now); err != nil {
			t.Fatalf("UpsertSubscription: %v", err)
		}
		if _, err := st.UpsertEntries(ctx, feedID, []store.EntryInput{{
			GUID:        "guid-1",
			URL:         "https://example.com/entry",
			Title:       "Entry",
			Body:        "body",
			BodyHash:    []byte("hash"),
			PublishedAt: now.Unix() + int64(i),
			UpdatedAt:   now.Unix(),
		}}, now); err != nil {
			t.Fatalf("UpsertEntries: %v", err)
		}
		feedIDs[i] = feedID
	}
	return st, feedIDs
}

func TestListEntriesByFolder(t *testing.T) {
	st, feedIDs := newTestStoreWithFeeds(t, 3)
	ctx := context.Background()

	folderID, err := st.GetOrCreateFolder(ctx, "Tech")
	if err != nil {
		t.Fatalf("GetOrCreateFolder: %v", err)
	}

	if err := st.UpdateSubscription(ctx, feedIDs[0], store.SubscriptionPatch{FolderID: &folderID}); err != nil {
		t.Fatalf("UpdateSubscription feed0: %v", err)
	}
	if err := st.UpdateSubscription(ctx, feedIDs[1], store.SubscriptionPatch{FolderID: &folderID}); err != nil {
		t.Fatalf("UpdateSubscription feed1: %v", err)
	}
	// feedIDs[2] stays unfiled.

	inFolder, err := st.ListEntriesByFolder(ctx, &folderID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntriesByFolder: %v", err)
	}
	if len(inFolder) != 2 {
		t.Fatalf("entries in folder = %+v, want 2", inFolder)
	}
	for _, e := range inFolder {
		if e.FeedID != feedIDs[0] && e.FeedID != feedIDs[1] {
			t.Fatalf("unexpected feed_id %d in folder result", e.FeedID)
		}
	}

	unfiled, err := st.ListEntriesByFolder(ctx, nil, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntriesByFolder(nil): %v", err)
	}
	if len(unfiled) != 1 || unfiled[0].FeedID != feedIDs[2] {
		t.Fatalf("unfiled entries = %+v, want just feed %d", unfiled, feedIDs[2])
	}
}

func TestListEntriesByRating(t *testing.T) {
	st, feedIDs := newTestStoreWithFeeds(t, 3)
	ctx := context.Background()

	rating5 := int64(5)
	if err := st.UpdateSubscription(ctx, feedIDs[0], store.SubscriptionPatch{Rating: &rating5}); err != nil {
		t.Fatalf("UpdateSubscription feed0: %v", err)
	}
	if err := st.UpdateSubscription(ctx, feedIDs[1], store.SubscriptionPatch{Rating: &rating5}); err != nil {
		t.Fatalf("UpdateSubscription feed1: %v", err)
	}
	// feedIDs[2] stays rating 0.

	rated, err := st.ListEntriesByRating(ctx, 5, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntriesByRating: %v", err)
	}
	if len(rated) != 2 {
		t.Fatalf("entries at rating 5 = %+v, want 2", rated)
	}

	unrated, err := st.ListEntriesByRating(ctx, 0, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntriesByRating(0): %v", err)
	}
	if len(unrated) != 1 || unrated[0].FeedID != feedIDs[2] {
		t.Fatalf("entries at rating 0 = %+v, want just feed %d", unrated, feedIDs[2])
	}
}

func TestListEntriesByFolderExcludesIgnored(t *testing.T) {
	st, _ := newTestStoreWithFeeds(t, 1)
	ctx := context.Background()
	now := time.Now()

	if err := st.AddIgnoreWord(ctx, "Entry", now); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}

	entries, err := st.ListEntriesByFolder(ctx, nil, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntriesByFolder: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none (hidden by ignore word)", entries)
	}
}
