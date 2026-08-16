package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestIgnoreWordHidesMatchingEntriesAndUnreadCount(t *testing.T) {
	st, feedID, _ := newTestStoreWithEntry(t, "https://example.com/a", "Baseball news", "the game was great")
	ctx := context.Background()
	now := time.Now()

	if err := st.UpsertSubscription(ctx, testUserID, feedID, nil, "", now); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	if err := st.AddIgnoreWord(ctx, testUserID, "Baseball", now); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none (hidden by ignore word)", entries)
	}

	subs, err := st.ListSubscriptionViews(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListSubscriptionViews: %v", err)
	}
	if len(subs) != 1 || subs[0].UnreadCount != 0 {
		t.Fatalf("subs = %+v, want unread_count 0", subs)
	}

	words, err := st.ListIgnoreWords(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListIgnoreWords: %v", err)
	}
	if len(words) != 1 || words[0].Word != "Baseball" {
		t.Fatalf("words = %+v, want one 'Baseball' entry", words)
	}

	if err := st.RemoveIgnoreWord(ctx, testUserID, words[0].ID); err != nil {
		t.Fatalf("RemoveIgnoreWord: %v", err)
	}

	entries, err = st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries (after remove): %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want the entry back after removing the ignore word", entries)
	}

	subs, err = st.ListSubscriptionViews(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListSubscriptionViews (after remove): %v", err)
	}
	if len(subs) != 1 || subs[0].UnreadCount != 1 {
		t.Fatalf("subs = %+v, want unread_count 1 after removing the ignore word", subs)
	}
}

func TestIgnoreWordAppliesToNewlyFetchedEntries(t *testing.T) {
	st, feedID, _ := newTestStoreWithEntry(t, "https://example.com/a", "Unrelated", "body")
	ctx := context.Background()
	now := time.Now()

	if err := st.AddIgnoreWord(ctx, testUserID, "spoiler", now); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}

	if _, err := st.UpsertEntries(ctx, feedID, []store.EntryInput{{
		GUID:        "guid-2",
		URL:         "https://example.com/b",
		Title:       "Big spoiler inside",
		Body:        "body",
		BodyHash:    []byte("hash-2"),
		PublishedAt: now.Unix(),
		UpdatedAt:   now.Unix(),
	}}, now); err != nil {
		t.Fatalf("UpsertEntries: %v", err)
	}

	entries, err := st.ListEntries(ctx, testUserID, feedID, false, 10, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Unrelated" {
		t.Fatalf("entries = %+v, want only the non-matching entry", entries)
	}
}

func TestAddIgnoreWordRejectsBlank(t *testing.T) {
	st, _, _ := newTestStoreWithEntry(t, "https://example.com/a", "Title", "body")
	ctx := context.Background()

	if err := st.AddIgnoreWord(ctx, testUserID, "   ", time.Now()); err == nil {
		t.Fatal("AddIgnoreWord(blank) = nil, want error")
	}
}

func TestRemoveIgnoreWordUnknownID(t *testing.T) {
	st, _, _ := newTestStoreWithEntry(t, "https://example.com/a", "Title", "body")
	ctx := context.Background()

	if err := st.RemoveIgnoreWord(ctx, testUserID, 999999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RemoveIgnoreWord(unknown) = %v, want ErrNotFound", err)
	}
}
