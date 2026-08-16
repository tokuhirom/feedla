package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

// Regression coverage for issue #75: a feed that carries no dates at all
// stamps every item with the same crawl-time PublishedAt, so without a
// guard a single crawl would dump its entire backlog in as unread, all
// sorted as "latest".
func TestUpsertEntriesDateMissingKeepsOnlyTopmostUnread(t *testing.T) {
	st, feedID := newGCTestStore(t)
	ctx := context.Background()
	now := time.Now()

	newCount, err := st.UpsertEntries(ctx, feedID, []store.EntryInput{
		{GUID: "a", URL: "https://example.com/a", Title: "A", Body: "body", BodyHash: []byte("ha"), PublishedAt: now.Unix(), UpdatedAt: now.Unix(), DateMissing: true},
		{GUID: "b", URL: "https://example.com/b", Title: "B", Body: "body", BodyHash: []byte("hb"), PublishedAt: now.Unix(), UpdatedAt: now.Unix(), DateMissing: true},
		{GUID: "c", URL: "https://example.com/c", Title: "C", Body: "body", BodyHash: []byte("hc"), PublishedAt: now.Unix(), UpdatedAt: now.Unix(), DateMissing: true},
	}, now)
	if err != nil {
		t.Fatalf("UpsertEntries: %v", err)
	}
	if newCount != 3 {
		t.Fatalf("newCount = %d, want 3", newCount)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, true, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].GUID != "a" {
		t.Fatalf("unread entries = %+v, want only guid=a (the topmost/feed-order-first DateMissing entry)", entries)
	}

	all, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries(includeRead): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all entries = %+v, want 3", all)
	}
	for _, e := range all {
		if e.GUID == "a" {
			continue
		}
		if e.ReadAt == nil {
			t.Errorf("entry %q: ReadAt is nil, want auto-marked read", e.GUID)
		}
	}
}

// A DateMissing entry that arrives alone in its own crawl batch (the common
// steady-state case for a dateless feed gaining one new item at a time)
// should stay unread, same as before this guard existed.
func TestUpsertEntriesDateMissingAloneStaysUnread(t *testing.T) {
	st, feedID := newGCTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if _, err := st.UpsertEntries(ctx, feedID, []store.EntryInput{
		{GUID: "a", URL: "https://example.com/a", Title: "A", Body: "body", BodyHash: []byte("ha"), PublishedAt: now.Unix(), UpdatedAt: now.Unix(), DateMissing: true},
	}, now); err != nil {
		t.Fatalf("UpsertEntries: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, true, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("unread entries = %+v, want the lone DateMissing entry to stay unread", entries)
	}
}
