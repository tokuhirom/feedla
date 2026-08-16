package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

// insertEntry inserts a single entry with an explicit published_at/fetched_at
// (UpsertEntries stamps fetched_at from the `now` argument, so callers who
// need distinct fetched_at values must call it once per entry).
func insertEntry(t *testing.T, st *store.Store, feedID int64, guid string, publishedAt, fetchedAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := st.UpsertEntries(ctx, feedID, []store.EntryInput{{
		GUID:        guid,
		URL:         "https://example.com/" + guid,
		Title:       guid,
		Body:        "body",
		BodyHash:    []byte("hash-" + guid),
		PublishedAt: publishedAt.Unix(),
		UpdatedAt:   publishedAt.Unix(),
	}}, fetchedAt); err != nil {
		t.Fatalf("UpsertEntries(%s): %v", guid, err)
	}

	var id int64
	if err := st.Read.QueryRowContext(ctx, `SELECT id FROM entries WHERE feed_id = ? AND guid = ?`, feedID, guid).Scan(&id); err != nil {
		t.Fatalf("lookup id for %s: %v", guid, err)
	}
	return id
}

func newGCTestStore(t *testing.T) (*store.Store, int64) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	feedID, err := st.UpsertFeed(ctx, "https://example.com/feed", "", "", 1800, time.Now())
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", time.Now()); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	return st, feedID
}

func TestDeleteOldReadEntriesSkipsPinnedAndUnread(t *testing.T) {
	st, feedID := newGCTestStore(t)
	ctx := context.Background()
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour)

	oldReadID := insertEntry(t, st, feedID, "old-read", old, old)
	oldPinnedID := insertEntry(t, st, feedID, "old-pinned", old, old)
	recentReadID := insertEntry(t, st, feedID, "recent-read", now, now)

	if _, err := st.MarkEntriesRead(ctx, testUserID, []int64{oldReadID, oldPinnedID, recentReadID}, now); err != nil {
		t.Fatalf("MarkEntriesRead: %v", err)
	}
	if err := st.AddPin(ctx, testUserID, oldPinnedID, now); err != nil {
		t.Fatalf("AddPin: %v", err)
	}

	deleted, err := st.DeleteOldReadEntries(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteOldReadEntries: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	for id, wantExists := range map[int64]bool{
		oldReadID:    false,
		oldPinnedID:  true,
		recentReadID: true,
	} {
		var exists bool
		if err := st.Read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id = ?)`, id).Scan(&exists); err != nil {
			t.Fatalf("check entry %d: %v", id, err)
		}
		if exists != wantExists {
			t.Fatalf("entry %d exists = %v, want %v", id, exists, wantExists)
		}
	}

	// Pinned entries must survive intact: cascading delete via the pins FK
	// would silently break "read later" if this regressed.
	pins, err := st.ListPins(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListPins: %v", err)
	}
	if len(pins) != 1 || pins[0].EntryID != oldPinnedID {
		t.Fatalf("pins = %+v, want the pinned entry to survive", pins)
	}
}

func TestTrimExcessEntriesKeepsPinnedAndNewest(t *testing.T) {
	st, feedID := newGCTestStore(t)
	ctx := context.Background()
	base := time.Now().Add(-10 * 24 * time.Hour)

	var ids []int64
	for i := range 5 {
		id := insertEntry(t, st, feedID, guidFor(i), base.Add(time.Duration(i)*time.Hour), base)
		ids = append(ids, id)
	}
	// ids[0] is the oldest (lowest published_at). Pin it so it survives
	// trimming despite ranking outside the retained window.
	if err := st.AddPin(ctx, testUserID, ids[0], base); err != nil {
		t.Fatalf("AddPin: %v", err)
	}
	if _, err := st.MarkEntriesRead(ctx, testUserID, ids, base); err != nil {
		t.Fatalf("MarkEntriesRead: %v", err)
	}

	// Keep only the newest 3; ids[0] (pinned) and ids[1] (unpinned, 2nd
	// oldest) rank outside that window, but only ids[1] should be deleted.
	deleted, err := st.TrimExcessEntries(ctx, 3)
	if err != nil {
		t.Fatalf("TrimExcessEntries: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	for i, id := range ids {
		wantExists := i != 1
		var exists bool
		if err := st.Read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id = ?)`, id).Scan(&exists); err != nil {
			t.Fatalf("check entry %d: %v", id, err)
		}
		if exists != wantExists {
			t.Fatalf("entry index %d (id %d) exists = %v, want %v", i, id, exists, wantExists)
		}
	}
}

func TestOptimize(t *testing.T) {
	st, _ := newGCTestStore(t)
	if err := st.Optimize(context.Background()); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
}

func guidFor(i int) string {
	return "trim-" + string(rune('a'+i))
}
