package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestFeedFulltextEnableGetDisable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, "https://example.com/feed.xml", "", "Example Feed", 3600, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}

	if _, err := st.GetFeedFulltext(ctx, feedID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetFeedFulltext before enable = %v, want ErrNotFound", err)
	}

	if err := st.EnableFeedFulltext(ctx, feedID, testUserID, now); err != nil {
		t.Fatalf("EnableFeedFulltext: %v", err)
	}
	// Re-enabling is idempotent, not an error.
	if err := st.EnableFeedFulltext(ctx, feedID, testUserID, now); err != nil {
		t.Fatalf("EnableFeedFulltext (again): %v", err)
	}

	f, err := st.GetFeedFulltext(ctx, feedID)
	if err != nil {
		t.Fatalf("GetFeedFulltext: %v", err)
	}
	if f.FeedID != feedID || f.CreatedBy != testUserID {
		t.Errorf("GetFeedFulltext = %+v, want feed_id=%d created_by=%d", f, feedID, testUserID)
	}

	if err := st.DisableFeedFulltext(ctx, feedID); err != nil {
		t.Fatalf("DisableFeedFulltext: %v", err)
	}
	if _, err := st.GetFeedFulltext(ctx, feedID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetFeedFulltext after disable = %v, want ErrNotFound", err)
	}
	// Disabling an already-disabled feed is not an error.
	if err := st.DisableFeedFulltext(ctx, feedID); err != nil {
		t.Fatalf("DisableFeedFulltext (again): %v", err)
	}
}

func TestSubscriptionViewFulltextField(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, "https://example.com/feed.xml", "", "Example Feed", 3600, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	view, err := st.GetSubscriptionView(ctx, testUserID, feedID)
	if err != nil {
		t.Fatalf("GetSubscriptionView: %v", err)
	}
	if view.Fulltext {
		t.Errorf("Fulltext = true before enabling, want false")
	}

	if err := st.EnableFeedFulltext(ctx, feedID, testUserID, now); err != nil {
		t.Fatalf("EnableFeedFulltext: %v", err)
	}

	view, err = st.GetSubscriptionView(ctx, testUserID, feedID)
	if err != nil {
		t.Fatalf("GetSubscriptionView: %v", err)
	}
	if !view.Fulltext {
		t.Errorf("Fulltext = false after enabling, want true")
	}

	views, err := st.ListSubscriptionViews(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListSubscriptionViews: %v", err)
	}
	if len(views) != 1 || !views[0].Fulltext {
		t.Errorf("ListSubscriptionViews = %+v, want one view with Fulltext=true", views)
	}
}
