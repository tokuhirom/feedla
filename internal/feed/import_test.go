package feed_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

// testUserID is the bootstrap admin (id=1), unconditionally seeded by
// migration 0005 on every fresh store.Open.
const testUserID int64 = 1

const sampleOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
  <head><title>subscriptions</title></head>
  <body>
    <outline text="Tech" title="Tech">
      <outline text="Feed A" title="Feed A" type="rss"
        xmlUrl="https://a.example.com/feed" htmlUrl="https://a.example.com/"/>
      <outline text="Feed B" title="Feed B" type="rss"
        xmlUrl="https://b.example.com/feed" htmlUrl="https://b.example.com/"/>
    </outline>
    <outline text="Feed C" title="Feed C" type="rss"
      xmlUrl="https://c.example.com/feed" htmlUrl="https://c.example.com/"/>
  </body>
</opml>`

// TestImportOPMLMaxFeedsLimit covers FR_QUOTA_OPML_MAX_FEEDS
// (docs/multi-user-design.md's リソース制限 section): a document over the
// limit is rejected atomically, before anything is written.
func TestImportOPMLMaxFeedsLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	if _, err := feed.ImportOPML(ctx, st, testUserID, strings.NewReader(sampleOPML), 2); err == nil {
		t.Fatal("ImportOPML with maxFeeds=2 over a 3-feed document: want an error")
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 0 {
		t.Fatalf("len(feeds) after a rejected import = %d, want 0 (no partial write)", len(feeds))
	}

	n, err := feed.ImportOPML(ctx, st, testUserID, strings.NewReader(sampleOPML), 3)
	if err != nil {
		t.Fatalf("ImportOPML with maxFeeds=3 at the limit: %v", err)
	}
	if n != 3 {
		t.Fatalf("imported = %d, want 3", n)
	}
}

func TestImportOPML(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	n, err := feed.ImportOPML(ctx, st, testUserID, strings.NewReader(sampleOPML), 0)
	if err != nil {
		t.Fatalf("ImportOPML: %v", err)
	}
	if n != 3 {
		t.Fatalf("imported = %d, want 3", n)
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 3 {
		t.Fatalf("len(feeds) = %d, want 3", len(feeds))
	}

	subs, err := st.ListSubscriptions(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("len(subs) = %d, want 3", len(subs))
	}

	folders, err := st.ListFolders(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "Tech" {
		t.Fatalf("folders = %+v, want single Tech folder", folders)
	}

	// Re-importing the same OPML must not create duplicate rows.
	if _, err := feed.ImportOPML(ctx, st, testUserID, strings.NewReader(sampleOPML), 0); err != nil {
		t.Fatalf("second ImportOPML: %v", err)
	}
	feeds, err = st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds after re-import: %v", err)
	}
	if len(feeds) != 3 {
		t.Fatalf("len(feeds) after re-import = %d, want 3", len(feeds))
	}
}

// TestImportOPMLSkipsPagewatchURLs verifies §12 #7: ExportOPML never emits a
// "pagewatch:" xmlUrl, so one can only appear in a hand-edited or foreign
// OPML file. Importing it verbatim would create a feeds row with no
// matching scrape_sources row, which crawlOne can't fetch — so it must be
// skipped instead.
func TestImportOPMLSkipsPagewatchURLs(t *testing.T) {
	const opmlWithPagewatchURL = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
  <head><title>subscriptions</title></head>
  <body>
    <outline text="Feed A" title="Feed A" type="rss"
      xmlUrl="https://a.example.com/feed" htmlUrl="https://a.example.com/"/>
    <outline text="Sneaky" title="Sneaky" type="rss"
      xmlUrl="pagewatch:https://b.example.com/diary/" htmlUrl="https://b.example.com/diary/"/>
    <outline text="Sneaky2" title="Sneaky2" type="rss"
      xmlUrl="selector:https://c.example.com/news/" htmlUrl="https://c.example.com/news/"/>
  </body>
</opml>`

	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	n, err := feed.ImportOPML(ctx, st, testUserID, strings.NewReader(opmlWithPagewatchURL), 0)
	if err != nil {
		t.Fatalf("ImportOPML: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported = %d, want 1 (the pagewatch:/selector: entries must be skipped)", n)
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("len(feeds) = %d, want 1", len(feeds))
	}
	if feeds[0].FeedURL != "https://a.example.com/feed" {
		t.Errorf("feeds[0].FeedURL = %q, want the non-pagewatch feed", feeds[0].FeedURL)
	}
}
