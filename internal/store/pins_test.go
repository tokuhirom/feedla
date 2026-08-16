package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func newTestStoreWithEntry(t *testing.T, url, title, body string) (*store.Store, int64, int64) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now()

	feedID, err := st.UpsertFeed(ctx, "https://example.com/feed", "", "", 1800, now)
	if err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	if _, err := st.UpsertEntries(ctx, feedID, []store.EntryInput{{
		GUID:        "guid-1",
		URL:         url,
		Title:       title,
		Body:        body,
		BodyHash:    []byte("hash"),
		PublishedAt: now.Unix(),
		UpdatedAt:   now.Unix(),
	}}, now); err != nil {
		t.Fatalf("UpsertEntries: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly one", entries)
	}
	return st, feedID, entries[0].ID
}

func TestPinAddRemoveList(t *testing.T) {
	st, _, entryID := newTestStoreWithEntry(t, "https://example.com/a", "Article A", "body")
	ctx := context.Background()
	now := time.Now()

	if err := st.AddPin(ctx, testUserID, entryID, now); err != nil {
		t.Fatalf("AddPin: %v", err)
	}
	// Adding twice must be a no-op, not an error.
	if err := st.AddPin(ctx, testUserID, entryID, now); err != nil {
		t.Fatalf("AddPin (again): %v", err)
	}

	pins, err := st.ListPins(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListPins: %v", err)
	}
	if len(pins) != 1 || pins[0].EntryID != entryID || pins[0].URL != "https://example.com/a" {
		t.Fatalf("pins = %+v, want one pin for entry %d", pins, entryID)
	}

	if err := st.RemovePin(ctx, testUserID, entryID); err != nil {
		t.Fatalf("RemovePin: %v", err)
	}
	if err := st.RemovePin(ctx, testUserID, entryID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RemovePin (again) = %v, want ErrNotFound", err)
	}

	pins, err = st.ListPins(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListPins: %v", err)
	}
	if len(pins) != 0 {
		t.Fatalf("pins = %+v, want empty after remove", pins)
	}
}

func TestAddPinUnknownEntry(t *testing.T) {
	st, _, _ := newTestStoreWithEntry(t, "https://example.com/a", "Article A", "body")
	ctx := context.Background()

	if err := st.AddPin(ctx, testUserID, 999999, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("AddPin(unknown) = %v, want ErrNotFound", err)
	}
}

func TestListEntriesReportsPinned(t *testing.T) {
	st, feedID, entryID := newTestStoreWithEntry(t, "https://example.com/a", "Article A", "body")
	ctx := context.Background()

	if err := st.AddPin(ctx, testUserID, entryID, time.Now()); err != nil {
		t.Fatalf("AddPin: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || !entries[0].Pinned {
		t.Fatalf("entries = %+v, want pinned entry", entries)
	}
}

func TestFindEntryByURL(t *testing.T) {
	st, _, entryID := newTestStoreWithEntry(t, "https://example.com/a", "Article A", "body")
	ctx := context.Background()

	id, err := st.FindEntryByURL(ctx, "https://example.com/a")
	if err != nil {
		t.Fatalf("FindEntryByURL: %v", err)
	}
	if id != entryID {
		t.Fatalf("id = %d, want %d", id, entryID)
	}

	if _, err := st.FindEntryByURL(ctx, "https://example.com/missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("FindEntryByURL(missing) = %v, want ErrNotFound", err)
	}
}

func TestSearchEntriesLongQueryUsesFTS(t *testing.T) {
	st, _, _ := newTestStoreWithEntry(t, "https://example.com/a", "Golang Concurrency Patterns", "about goroutines and channels")
	ctx := context.Background()

	entries, err := st.SearchEntries(ctx, testUserID, "goroutines", 10, nil)
	if err != nil {
		t.Fatalf("SearchEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Golang Concurrency Patterns" {
		t.Fatalf("entries = %+v, want the matching article", entries)
	}

	noMatch, err := st.SearchEntries(ctx, testUserID, "nonexistentterm", 10, nil)
	if err != nil {
		t.Fatalf("SearchEntries (no match): %v", err)
	}
	if len(noMatch) != 0 {
		t.Fatalf("noMatch = %+v, want empty", noMatch)
	}
}

func TestSearchEntriesShortQueryUsesLike(t *testing.T) {
	st, _, _ := newTestStoreWithEntry(t, "https://example.com/a", "Go tips", "short body")
	ctx := context.Background()

	entries, err := st.SearchEntries(ctx, testUserID, "Go", 10, nil)
	if err != nil {
		t.Fatalf("SearchEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Go tips" {
		t.Fatalf("entries = %+v, want the matching article", entries)
	}
}
