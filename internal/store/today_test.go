package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestListTodayEntries(t *testing.T) {
	st, feedIDs := newTestStoreWithFeeds(t, 3)
	ctx := context.Background()
	now := time.Now()

	rating5 := int64(5)
	if err := st.UpdateSubscription(ctx, testUserID, feedIDs[0], store.SubscriptionPatch{Rating: &rating5}); err != nil {
		t.Fatalf("UpdateSubscription feed0: %v", err)
	}
	// feedIDs[1] and feedIDs[2] stay rating 0.

	// An old entry (outside the 24h window) on feedIDs[1], and mark
	// feedIDs[2]'s fixture entry read so it's excluded from Today too.
	if _, err := st.UpsertEntries(ctx, feedIDs[1], []store.EntryInput{{
		GUID:        "old",
		URL:         "https://example.com/old",
		Title:       "Old",
		Body:        "body",
		BodyHash:    []byte("hold"),
		PublishedAt: now.Add(-48 * time.Hour).Unix(),
		UpdatedAt:   now.Unix(),
	}}, now); err != nil {
		t.Fatalf("UpsertEntries old: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedIDs[2], true, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries feed2: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("feed2 unread entries = %+v, want 1", entries)
	}
	if _, err := st.MarkEntriesRead(ctx, testUserID, []int64{entries[0].ID}, now); err != nil {
		t.Fatalf("MarkEntriesRead: %v", err)
	}

	since := now.Add(-24 * time.Hour).Unix()
	today, err := st.ListTodayEntries(ctx, testUserID, since, 10, nil)
	if err != nil {
		t.Fatalf("ListTodayEntries: %v", err)
	}
	// Only feedIDs[0]'s fixture entry (rating 5, unread, within window)
	// should show up: feedIDs[1]'s fixture entry is old (its "old" guid is
	// outside the window; its original fixture entry from
	// newTestStoreWithFeeds is within the window but let's check both).
	if len(today) != 2 {
		t.Fatalf("today entries = %+v, want 2 (feed0 fixture + feed1 fixture, both unread & within window)", today)
	}
	for _, e := range today {
		if e.FeedID != feedIDs[0] && e.FeedID != feedIDs[1] {
			t.Fatalf("unexpected feed_id %d in today result", e.FeedID)
		}
		if e.GUID == "old" {
			t.Fatalf("today result includes an entry outside the 24h window: %+v", e)
		}
	}

	// Newest first.
	if len(today) == 2 && today[0].PublishedAt < today[1].PublishedAt {
		t.Fatalf("today entries not newest-first: %+v", today)
	}
}

func TestListTodayEntriesExcludesIgnored(t *testing.T) {
	st, _ := newTestStoreWithFeeds(t, 1)
	ctx := context.Background()
	now := time.Now()

	if err := st.AddIgnoreWord(ctx, testUserID, "Entry", now); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}

	since := now.Add(-24 * time.Hour).Unix()
	entries, err := st.ListTodayEntries(ctx, testUserID, since, 10, nil)
	if err != nil {
		t.Fatalf("ListTodayEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none (hidden by ignore word)", entries)
	}
}

func TestCountTodayUnread(t *testing.T) {
	st, _ := newTestStoreWithFeeds(t, 2)
	ctx := context.Background()
	now := time.Now()

	since := now.Add(-24 * time.Hour).Unix()

	count, err := st.CountTodayUnread(ctx, testUserID, since)
	if err != nil {
		t.Fatalf("CountTodayUnread: %v", err)
	}
	entries, err := st.ListTodayEntries(ctx, testUserID, since, 100, nil)
	if err != nil {
		t.Fatalf("ListTodayEntries: %v", err)
	}
	if int(count) != len(entries) {
		t.Fatalf("CountTodayUnread = %d, want %d (matching ListTodayEntries)", count, len(entries))
	}
	if count != 2 {
		t.Fatalf("CountTodayUnread = %d, want 2", count)
	}

	// Marking one read should decrement the count.
	if _, err := st.MarkEntriesRead(ctx, testUserID, []int64{entries[0].ID}, now); err != nil {
		t.Fatalf("MarkEntriesRead: %v", err)
	}
	count, err = st.CountTodayUnread(ctx, testUserID, since)
	if err != nil {
		t.Fatalf("CountTodayUnread after mark read: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountTodayUnread after mark read = %d, want 1", count)
	}
}
