package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestAPITokenLifecycle(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()
	hash := []byte("token-hash-abc")

	tok, err := st.CreateAPIToken(ctx, 1, "my client", hash, now)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if tok.ID == 0 || tok.Label != "my client" {
		t.Fatalf("token = %+v", tok)
	}

	got, err := st.GetAPITokenByHash(ctx, hash)
	if err != nil {
		t.Fatalf("GetAPITokenByHash: %v", err)
	}
	if got.User.ID != 1 || got.ID != tok.ID {
		t.Fatalf("got = %+v", got)
	}

	if err := st.TouchAPITokenLastUsed(ctx, tok.ID, now); err != nil {
		t.Fatalf("TouchAPITokenLastUsed: %v", err)
	}

	list, err := st.ListAPITokensForUser(ctx, 1)
	if err != nil {
		t.Fatalf("ListAPITokensForUser: %v", err)
	}
	if len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatalf("list = %+v, want 1 token with last_used_at set", list)
	}

	if err := st.DeleteAPIToken(ctx, 1, tok.ID); err != nil {
		t.Fatalf("DeleteAPIToken: %v", err)
	}
	if _, err := st.GetAPITokenByHash(ctx, hash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteAPITokenWrongUserScoped(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	tok, err := st.CreateAPIToken(ctx, 1, "label", []byte("hash-x"), now)
	if err != nil {
		t.Fatal(err)
	}

	// Deleting as a different user must not remove someone else's token.
	if err := st.DeleteAPIToken(ctx, 2, tok.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (not authorized)", err)
	}
	if _, err := st.GetAPITokenByHash(ctx, []byte("hash-x")); err != nil {
		t.Fatalf("token should still exist: %v", err)
	}
}

func TestGetAPITokenUnknownHash(t *testing.T) {
	st := newAuthTestStore(t)
	if _, err := st.GetAPITokenByHash(context.Background(), []byte("nope")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
