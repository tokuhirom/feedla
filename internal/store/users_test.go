package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func newAuthTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestMigrationSeedsLockedAdmin(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()

	u, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.ID != 1 || !u.IsAdmin || u.PasswordHash != store.SetupSentinelHash {
		t.Fatalf("seeded admin = %+v, want id=1 is_admin=true password_hash=%q", u, store.SetupSentinelHash)
	}

	// Case-insensitive lookup (COLLATE NOCASE on the column).
	if _, err := st.GetUserByUsername(ctx, "ADMIN"); err != nil {
		t.Fatalf("GetUserByUsername(uppercase): %v", err)
	}

	pending, err := st.IsSetupPending(ctx, u.ID)
	if err != nil {
		t.Fatalf("IsSetupPending: %v", err)
	}
	if !pending {
		t.Fatal("freshly migrated admin should have setup pending")
	}
}

func TestGetUserNotFound(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()

	if _, err := st.GetUserByUsername(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := st.GetUserByID(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCompleteSetupOnlyOnce(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := st.CompleteSetup(ctx, 1, "admin", "$argon2id$fake$hash", now); err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}

	pending, err := st.IsSetupPending(ctx, 1)
	if err != nil {
		t.Fatalf("IsSetupPending: %v", err)
	}
	if pending {
		t.Fatal("setup should no longer be pending")
	}

	// A second call must fail: the sentinel hash is gone, so the WHERE
	// clause matches nothing.
	if err := st.CompleteSetup(ctx, 1, "admin", "$argon2id$another$hash", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second CompleteSetup err = %v, want ErrNotFound", err)
	}
}

func TestUpdateUserPassword(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := st.UpdateUserPassword(ctx, 1, "$argon2id$new$hash", now); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	u, err := st.GetUserByID(ctx, 1)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if u.PasswordHash != "$argon2id$new$hash" {
		t.Fatalf("PasswordHash = %q, want updated hash", u.PasswordHash)
	}

	if err := st.UpdateUserPassword(ctx, 999, "x", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
