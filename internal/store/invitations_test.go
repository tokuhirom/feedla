package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/auth"
	"github.com/tokuhirom/feedla/internal/store"
)

func TestAcceptInvitation(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	admin, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	_, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	inv, err := st.CreateInvitation(ctx, admin.ID, hash, now.Add(72*time.Hour), now)
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if inv.CreatedBy != admin.ID || inv.UsedBy != nil {
		t.Fatalf("CreateInvitation = %+v, want CreatedBy=%d UsedBy=nil", inv, admin.ID)
	}

	if err := st.CheckInvitation(ctx, hash, now); err != nil {
		t.Fatalf("CheckInvitation on fresh token: %v", err)
	}

	passwordHash, err := auth.HashPassword("invitee-password-123456")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u, err := st.AcceptInvitation(ctx, hash, "invitee", passwordHash, now)
	if err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	if u.Username != "invitee" || u.IsAdmin {
		t.Fatalf("AcceptInvitation created user = %+v, want invitee/non-admin", u)
	}

	// The token is now spent: neither another accept nor a status check
	// succeeds.
	if err := st.CheckInvitation(ctx, hash, now); !errors.Is(err, store.ErrInvitationInvalid) {
		t.Fatalf("CheckInvitation on used token err = %v, want ErrInvitationInvalid", err)
	}
	if _, err := st.AcceptInvitation(ctx, hash, "someone-else", passwordHash, now); !errors.Is(err, store.ErrInvitationInvalid) {
		t.Fatalf("re-accept err = %v, want ErrInvitationInvalid", err)
	}
}

func TestAcceptInvitationExpired(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	admin, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	_, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	// Issued in the past, already expired by "now".
	if _, err := st.CreateInvitation(ctx, admin.ID, hash, now.Add(-time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if err := st.CheckInvitation(ctx, hash, now); !errors.Is(err, store.ErrInvitationInvalid) {
		t.Fatalf("CheckInvitation on expired token err = %v, want ErrInvitationInvalid", err)
	}

	passwordHash, err := auth.HashPassword("invitee-password-123456")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := st.AcceptInvitation(ctx, hash, "too-late", passwordHash, now); !errors.Is(err, store.ErrInvitationInvalid) {
		t.Fatalf("AcceptInvitation on expired token err = %v, want ErrInvitationInvalid", err)
	}
}

func TestAcceptInvitationUnknownToken(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()

	if err := st.CheckInvitation(ctx, []byte("does-not-exist"), time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CheckInvitation on unknown token err = %v, want ErrNotFound", err)
	}

	passwordHash, err := auth.HashPassword("invitee-password-123456")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := st.AcceptInvitation(ctx, []byte("does-not-exist"), "nobody", passwordHash, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("AcceptInvitation on unknown token err = %v, want ErrNotFound", err)
	}
}

func TestAcceptInvitationDuplicateUsername(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	admin, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	_, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if _, err := st.CreateInvitation(ctx, admin.ID, hash, now.Add(72*time.Hour), now); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	passwordHash, err := auth.HashPassword("invitee-password-123456")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	// "admin" already exists (the seeded bootstrap user).
	if _, err := st.AcceptInvitation(ctx, hash, "admin", passwordHash, now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("AcceptInvitation with duplicate username err = %v, want ErrConflict", err)
	}
}

func TestListInvitations(t *testing.T) {
	st := newAuthTestStore(t)
	ctx := context.Background()
	now := time.Now()

	admin, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	if invs, err := st.ListInvitations(ctx); err != nil || len(invs) != 0 {
		t.Fatalf("ListInvitations on empty store = %+v, %v, want empty slice", invs, err)
	}

	_, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if _, err := st.CreateInvitation(ctx, admin.ID, hash, now.Add(72*time.Hour), now); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	invs, err := st.ListInvitations(ctx)
	if err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
	if len(invs) != 1 || invs[0].CreatedBy != admin.ID {
		t.Fatalf("ListInvitations = %+v, want 1 invitation created by %d", invs, admin.ID)
	}
}
