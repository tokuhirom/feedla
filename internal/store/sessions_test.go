package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestSessionLifecycle(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()
	tokenHash := []byte("fake-hash-1234567890")

	sess, err := st.CreateSession(ctx, 1, tokenHash, now, now.Add(90*24*time.Hour))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID == 0 || sess.UserID != 1 {
		t.Fatalf("session = %+v", sess)
	}

	got, err := st.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if got.User.ID != 1 || got.ID != sess.ID {
		t.Fatalf("got = %+v", got)
	}

	if err := st.TouchSession(ctx, sess.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	if err := st.DeleteSession(ctx, tokenHash); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := st.GetSessionByTokenHash(ctx, tokenHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err after delete = %v, want ErrNotFound", err)
	}
}

func TestGetSessionUnknownHash(t *testing.T) {
	st := newAuthTestStore(t)
	if _, err := st.GetSessionByTokenHash(context.Background(), []byte("nope")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteAllSessionsForUser(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	h1, h2 := []byte("hash-1"), []byte("hash-2")
	if _, err := st.CreateSession(ctx, 1, h1, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(ctx, 1, h2, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteAllSessionsForUser(ctx, 1); err != nil {
		t.Fatalf("DeleteAllSessionsForUser: %v", err)
	}

	for _, h := range [][]byte{h1, h2} {
		if _, err := st.GetSessionByTokenHash(ctx, h); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session %x still present after DeleteAllSessionsForUser", h)
		}
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	expired := []byte("expired-hash")
	live := []byte("live-hash")
	if _, err := st.CreateSession(ctx, 1, expired, now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(ctx, 1, live, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	n, err := st.DeleteExpiredSessions(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}

	if _, err := st.GetSessionByTokenHash(ctx, expired); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("expired session should be gone")
	}
	if _, err := st.GetSessionByTokenHash(ctx, live); err != nil {
		t.Fatalf("live session should remain: %v", err)
	}
}
