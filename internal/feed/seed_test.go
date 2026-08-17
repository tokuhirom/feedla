package feed_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

func TestSeedIfEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()

	n, err := feed.SeedIfEmpty(ctx, st)
	if err != nil {
		t.Fatalf("SeedIfEmpty: %v", err)
	}
	if n == 0 {
		t.Fatalf("SeedIfEmpty on an empty store imported 0 feeds, want > 0")
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != n {
		t.Fatalf("len(feeds) = %d, want %d", len(feeds), n)
	}

	// A second call must be a no-op: it shouldn't re-seed or duplicate
	// feeds once any subscription already exists (e.g. the user deleted
	// the seeded feed and restarted).
	n2, err := feed.SeedIfEmpty(ctx, st)
	if err != nil {
		t.Fatalf("SeedIfEmpty (second call): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("SeedIfEmpty on a non-empty store imported %d feeds, want 0", n2)
	}

	feeds, err = st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != n {
		t.Fatalf("len(feeds) after second SeedIfEmpty = %d, want unchanged %d", len(feeds), n)
	}
}

func TestSeedIfEmptySkipsWhenFeedsExist(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()

	const otherOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
  <head><title>subscriptions</title></head>
  <body>
    <outline text="Feed A" title="Feed A" type="rss"
      xmlUrl="https://a.example.com/feed" htmlUrl="https://a.example.com/"/>
  </body>
</opml>`
	if _, err := feed.ImportOPML(ctx, st, testUserID, strings.NewReader(otherOPML), 0); err != nil {
		t.Fatalf("ImportOPML: %v", err)
	}

	n, err := feed.SeedIfEmpty(ctx, st)
	if err != nil {
		t.Fatalf("SeedIfEmpty: %v", err)
	}
	if n != 0 {
		t.Fatalf("SeedIfEmpty with a pre-existing feed imported %d feeds, want 0", n)
	}

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("len(feeds) = %d, want 1 (unchanged)", len(feeds))
	}
}
