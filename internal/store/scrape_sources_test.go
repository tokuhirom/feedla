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

func newTestStoreWithScrapeSource(t *testing.T) (*store.Store, int64, int64) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, "pagewatch:https://example.com/diary/", "", "", 3600, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	srcID, err := st.CreateScrapeSource(ctx, testUserID, feedID, "pagewatch", "https://example.com/diary/", nil, now)
	if err != nil {
		t.Fatalf("CreateScrapeSource: %v", err)
	}
	return st, feedID, srcID
}

func TestScrapeSourceCreateGet(t *testing.T) {
	st, feedID, srcID := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	byID, err := st.GetScrapeSource(ctx, srcID)
	if err != nil {
		t.Fatalf("GetScrapeSource: %v", err)
	}
	if byID.FeedID != feedID || byID.Kind != "pagewatch" || byID.TargetURL != "https://example.com/diary/" {
		t.Fatalf("byID = %+v, want feed %d / kind pagewatch / target https://example.com/diary/", byID, feedID)
	}
	if string(byID.Config) != "{}" {
		t.Errorf("Config = %s, want default {} when created with nil config", byID.Config)
	}
	if byID.State != nil {
		t.Errorf("State = %s, want nil before any crawl", byID.State)
	}

	byFeed, err := st.GetScrapeSourceByFeedID(ctx, feedID)
	if err != nil {
		t.Fatalf("GetScrapeSourceByFeedID: %v", err)
	}
	if byFeed.ID != srcID {
		t.Errorf("byFeed.ID = %d, want %d", byFeed.ID, srcID)
	}
}

func TestScrapeSourceSubscriptionViewKind(t *testing.T) {
	st, feedID, _ := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", time.Now()); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	view, err := st.GetSubscriptionView(ctx, testUserID, feedID)
	if err != nil {
		t.Fatalf("GetSubscriptionView: %v", err)
	}
	if view.Kind != "pagewatch" {
		t.Errorf("Kind = %q, want pagewatch", view.Kind)
	}
	if view.FeedURL != "https://example.com/diary/" {
		t.Errorf("FeedURL = %q, want the pagewatch: prefix stripped", view.FeedURL)
	}

	views, err := st.ListSubscriptionViews(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListSubscriptionViews: %v", err)
	}
	if len(views) != 1 || views[0].Kind != "pagewatch" {
		t.Fatalf("views = %+v, want one pagewatch-kind view", views)
	}

	normalFeedID, err := st.UpsertFeed(ctx, "https://example.com/feed.xml", "", "", 1800, time.Now())
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, normalFeedID, nil, "", time.Now()); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	normalView, err := st.GetSubscriptionView(ctx, testUserID, normalFeedID)
	if err != nil {
		t.Fatalf("GetSubscriptionView: %v", err)
	}
	if normalView.Kind != "feed" {
		t.Errorf("Kind = %q, want feed for a normally-fetched feed", normalView.Kind)
	}
}

func TestScrapeSourceGetNotFound(t *testing.T) {
	st, _, _ := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	if _, err := st.GetScrapeSource(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetScrapeSource(missing) = %v, want ErrNotFound", err)
	}
	if _, err := st.GetScrapeSourceByFeedID(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetScrapeSourceByFeedID(missing) = %v, want ErrNotFound", err)
	}
}

func TestScrapeSourceFeedIDIsUnique(t *testing.T) {
	st, feedID, _ := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	if _, err := st.CreateScrapeSource(ctx, testUserID, feedID, "pagewatch", "https://example.com/other/", nil, time.Now()); err == nil {
		t.Error("CreateScrapeSource: want an error creating a second source for the same feed (feed_id is UNIQUE)")
	}
}

func TestScrapeSourceList(t *testing.T) {
	st, _, srcID := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	sources, err := st.ListScrapeSources(ctx)
	if err != nil {
		t.Fatalf("ListScrapeSources: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != srcID {
		t.Fatalf("sources = %+v, want exactly the one created source", sources)
	}
}

func TestScrapeSourceUpdateConfig(t *testing.T) {
	st, _, srcID := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	cfg := json.RawMessage(`{"ignore_patterns":["Document ID: [A-Za-z0-9]+"]}`)
	if err := st.UpdateScrapeSourceConfig(ctx, srcID, cfg, time.Now()); err != nil {
		t.Fatalf("UpdateScrapeSourceConfig: %v", err)
	}

	got, err := st.GetScrapeSource(ctx, srcID)
	if err != nil {
		t.Fatalf("GetScrapeSource: %v", err)
	}
	if string(got.Config) != string(cfg) {
		t.Errorf("Config = %s, want %s", got.Config, cfg)
	}

	if err := st.UpdateScrapeSourceConfig(ctx, 999999, cfg, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateScrapeSourceConfig(missing) = %v, want ErrNotFound", err)
	}
}

func TestScrapeSourceUpdateState(t *testing.T) {
	st, feedID, srcID := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	state := json.RawMessage(`{"version":1,"rules_version":1,"content_hash":"abc","blocks":[]}`)
	if err := st.UpdateScrapeSourceState(ctx, feedID, state, time.Now()); err != nil {
		t.Fatalf("UpdateScrapeSourceState: %v", err)
	}

	got, err := st.GetScrapeSource(ctx, srcID)
	if err != nil {
		t.Fatalf("GetScrapeSource: %v", err)
	}
	if string(got.State) != string(state) {
		t.Errorf("State = %s, want %s", got.State, state)
	}

	if err := st.UpdateScrapeSourceState(ctx, 999999, state, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateScrapeSourceState(missing feed) = %v, want ErrNotFound", err)
	}
}

// TestUnsubscribeLeavesScrapeSourceIntact confirms Unsubscribe -- which
// only removes the caller's subscriptions/user_entry_state rows, per Phase
// B's data model (feeds/entries/scrape_sources are shared across users) --
// does not cascade away the feed's scrape_sources row. Deleting a feed with
// zero remaining subscribers (which *does* cascade-delete its
// scrape_sources row via ON DELETE CASCADE, verified below directly against
// the feeds table) is a separate GC concern deferred to Phase C.
func TestUnsubscribeLeavesScrapeSourceIntact(t *testing.T) {
	st, feedID, srcID := newTestStoreWithScrapeSource(t)
	ctx := context.Background()

	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", time.Now()); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	if err := st.Unsubscribe(ctx, testUserID, feedID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if _, err := st.GetScrapeSource(ctx, srcID); err != nil {
		t.Errorf("GetScrapeSource after Unsubscribe = %v, want it to survive (feeds/scrape_sources are shared)", err)
	}

	if _, err := st.Write.ExecContext(ctx, `DELETE FROM feeds WHERE id = ?`, feedID); err != nil {
		t.Fatalf("delete feed directly: %v", err)
	}
	if _, err := st.GetScrapeSource(ctx, srcID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetScrapeSource after feed deletion = %v, want ErrNotFound (ON DELETE CASCADE)", err)
	}
}
