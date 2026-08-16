package feed_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

func TestExportOPMLRoundTrips(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	if _, err := feed.ImportOPML(ctx, st, testUserID, strings.NewReader(sampleOPML)); err != nil {
		t.Fatalf("ImportOPML: %v", err)
	}

	out, err := feed.ExportOPML(ctx, st, testUserID)
	if err != nil {
		t.Fatalf("ExportOPML: %v", err)
	}
	if !bytes.HasPrefix(out, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
		t.Fatalf("export missing xml declaration: %s", out)
	}

	dbPath2 := filepath.Join(t.TempDir(), "feedla2.db")
	st2, err := store.Open(dbPath2)
	if err != nil {
		t.Fatalf("store.Open (2): %v", err)
	}
	t.Cleanup(func() { st2.Close() })

	n, err := feed.ImportOPML(ctx, st2, testUserID, bytes.NewReader(out))
	if err != nil {
		t.Fatalf("ImportOPML(exported): %v", err)
	}
	if n != 3 {
		t.Fatalf("re-imported = %d, want 3", n)
	}

	folders, err := st2.ListFolders(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "Tech" {
		t.Fatalf("folders = %+v, want single Tech folder", folders)
	}

	feeds, err := st2.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 3 {
		t.Fatalf("len(feeds) = %d, want 3", len(feeds))
	}
}

// TestExportOPMLExcludesPagewatch verifies §12 #7: a pagewatch subscription
// has no real feed URL to offer another OPML-reading tool (feed_url is a
// "pagewatch:" pseudo-scheme meaningful only inside feedla), so it must be
// left out of the export entirely rather than round-tripped as a broken
// xmlUrl.
func TestExportOPMLExcludesPagewatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	feedFeedID, err := st.UpsertFeed(ctx, "https://example.com/feed.xml", "", "普通のフィード", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedFeedID, nil, "普通のフィード", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	pwFeedID, err := st.UpsertFeed(ctx, "pagewatch:https://example.com/diary/", "", "日記", 3600, now)
	if err != nil {
		t.Fatalf("UpsertFeed (pagewatch): %v", err)
	}
	if _, err := st.CreateScrapeSource(ctx, testUserID, pwFeedID, "pagewatch", "https://example.com/diary/", nil, now); err != nil {
		t.Fatalf("CreateScrapeSource: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, pwFeedID, nil, "日記", now); err != nil {
		t.Fatalf("UpsertSubscription (pagewatch): %v", err)
	}

	out, err := feed.ExportOPML(ctx, st, testUserID)
	if err != nil {
		t.Fatalf("ExportOPML: %v", err)
	}
	if strings.Contains(string(out), "pagewatch:") {
		t.Fatalf("export must not contain the pagewatch: pseudo-scheme: %s", out)
	}
	if !strings.Contains(string(out), "example.com/feed.xml") {
		t.Fatalf("export must still contain the normal feed: %s", out)
	}
}
