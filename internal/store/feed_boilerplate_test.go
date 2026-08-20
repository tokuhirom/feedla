package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestFeedBoilerplatePutGetDelete(t *testing.T) {
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

	if _, err := st.GetFeedBoilerplate(ctx, feedID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetFeedBoilerplate before any write = %v, want ErrNotFound", err)
	}

	first := json.RawMessage(`{"v":1,"pages":2,"counts":{"aaaaaaaaaaaaaaaa":[2,2]}}`)
	if err := st.PutFeedBoilerplate(ctx, feedID, first, now); err != nil {
		t.Fatalf("PutFeedBoilerplate: %v", err)
	}
	got, err := st.GetFeedBoilerplate(ctx, feedID)
	if err != nil {
		t.Fatalf("GetFeedBoilerplate: %v", err)
	}
	if string(got) != string(first) {
		t.Errorf("state = %s, want %s", got, first)
	}

	second := json.RawMessage(`{"v":1,"pages":3,"counts":{"aaaaaaaaaaaaaaaa":[3,3]}}`)
	if err := st.PutFeedBoilerplate(ctx, feedID, second, now.Add(time.Minute)); err != nil {
		t.Fatalf("PutFeedBoilerplate (update): %v", err)
	}
	got, err = st.GetFeedBoilerplate(ctx, feedID)
	if err != nil {
		t.Fatalf("GetFeedBoilerplate after update: %v", err)
	}
	if string(got) != string(second) {
		t.Errorf("state after update = %s, want %s", got, second)
	}

	if err := st.DeleteFeedBoilerplate(ctx, feedID); err != nil {
		t.Fatalf("DeleteFeedBoilerplate: %v", err)
	}
	if _, err := st.GetFeedBoilerplate(ctx, feedID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetFeedBoilerplate after delete = %v, want ErrNotFound", err)
	}
	// Deleting again is not an error.
	if err := st.DeleteFeedBoilerplate(ctx, feedID); err != nil {
		t.Fatalf("DeleteFeedBoilerplate (again): %v", err)
	}
}

func TestDisableFeedFulltextDiscardsBoilerplateState(t *testing.T) {
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
	if err := st.EnableFeedFulltext(ctx, feedID, testUserID, now); err != nil {
		t.Fatalf("EnableFeedFulltext: %v", err)
	}
	if err := st.PutFeedBoilerplate(ctx, feedID, json.RawMessage(`{"v":1,"pages":9,"counts":{}}`), now); err != nil {
		t.Fatalf("PutFeedBoilerplate: %v", err)
	}

	if err := st.DisableFeedFulltext(ctx, feedID); err != nil {
		t.Fatalf("DisableFeedFulltext: %v", err)
	}
	// Learned chrome only means anything alongside fulltext extraction, and
	// a re-enable months later should not start out stripping subtrees
	// learned from a version of the site that may be long gone.
	if _, err := st.GetFeedBoilerplate(ctx, feedID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetFeedBoilerplate after disable = %v, want ErrNotFound", err)
	}
}

func TestDeletingFeedCascadesToBoilerplateState(t *testing.T) {
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
	if err := st.PutFeedBoilerplate(ctx, feedID, json.RawMessage(`{"v":1,"pages":1,"counts":{}}`), now); err != nil {
		t.Fatalf("PutFeedBoilerplate: %v", err)
	}

	// No subscription was ever created, so the feed is orphaned.
	if _, err := st.DeleteOrphanFeeds(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("DeleteOrphanFeeds: %v", err)
	}
	if _, err := st.GetFeedBoilerplate(ctx, feedID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetFeedBoilerplate after the feed was deleted = %v, want ErrNotFound", err)
	}
}
