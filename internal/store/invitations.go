package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Invitation is an admin-issued, single-use signup token
// (docs/multi-user-design.md's 招待トークン制). Only its SHA-256 hash is
// persisted (see internal/auth.GenerateToken/HashToken) -- the raw token is
// shown to the issuing admin exactly once, at creation time.
type Invitation struct {
	ID        int64  `json:"id"`
	CreatedBy int64  `json:"created_by"`
	ExpiresAt int64  `json:"expires_at"`
	UsedBy    *int64 `json:"used_by"`
	UsedAt    *int64 `json:"used_at"`
	CreatedAt int64  `json:"created_at"`
}

// CreateInvitation inserts a new invitation issued by createdBy, expiring
// at expiresAt.
func (s *Store) CreateInvitation(ctx context.Context, createdBy int64, tokenHash []byte, expiresAt, now time.Time) (Invitation, error) {
	res, err := s.Write.ExecContext(ctx, `
		INSERT INTO invitations (token_hash, created_by, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`, tokenHash, createdBy, expiresAt.Unix(), now.Unix())
	if err != nil {
		return Invitation{}, fmt.Errorf("store: create invitation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Invitation{}, fmt.Errorf("store: create invitation: %w", err)
	}
	return Invitation{ID: id, CreatedBy: createdBy, ExpiresAt: expiresAt.Unix(), CreatedAt: now.Unix()}, nil
}

// ListInvitations returns every invitation (used or not, expired or not),
// newest first. Admin-only: callers must check the caller's own is_admin
// before exposing this. The raw token is never stored, so this can't leak
// still-usable credentials -- only issuance/use metadata.
func (s *Store) ListInvitations(ctx context.Context) ([]Invitation, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT id, created_by, expires_at, used_by, used_at, created_at
		FROM invitations ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list invitations: %w", err)
	}
	defer rows.Close()

	out := []Invitation{}
	for rows.Next() {
		var inv Invitation
		if err := rows.Scan(&inv.ID, &inv.CreatedBy, &inv.ExpiresAt, &inv.UsedBy, &inv.UsedAt, &inv.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan invitation: %w", err)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// CheckInvitation reports whether tokenHash refers to a still-redeemable
// invitation, without mutating anything. Used by the accept screen to
// decide whether to show the signup form or an "expired/used" message
// before the visitor types anything.
func (s *Store) CheckInvitation(ctx context.Context, tokenHash []byte, now time.Time) error {
	var expiresAt int64
	var usedBy sql.NullInt64
	err := s.Read.QueryRowContext(ctx, `
		SELECT expires_at, used_by FROM invitations WHERE token_hash = ?
	`, tokenHash).Scan(&expiresAt, &usedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: check invitation: %w", ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("store: check invitation: %w", err)
	}
	if usedBy.Valid || now.Unix() > expiresAt {
		return fmt.Errorf("store: check invitation: %w", ErrInvitationInvalid)
	}
	return nil
}

// AcceptInvitation validates tokenHash (must exist, be unused, and not
// expired), then atomically creates a new account and marks the invitation
// used. The final UPDATE is conditioned on used_by still being NULL, so a
// token can never be redeemed twice even if two requests race between the
// initial SELECT and it -- the loser gets ErrInvitationInvalid and its
// tentative user row is rolled back along with everything else.
//
// Returns ErrNotFound if no invitation matches tokenHash, ErrInvitationInvalid
// if it's expired, already used, or lost the race described above, or
// ErrConflict if username is already taken.
func (s *Store) AcceptInvitation(ctx context.Context, tokenHash []byte, username, passwordHash string, now time.Time) (User, error) {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("store: accept invitation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var invID int64
	var expiresAt int64
	var usedBy sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id, expires_at, used_by FROM invitations WHERE token_hash = ?
	`, tokenHash).Scan(&invID, &expiresAt, &usedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("store: accept invitation: %w", ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: accept invitation: %w", err)
	}
	if usedBy.Valid || now.Unix() > expiresAt {
		return User{}, fmt.Errorf("store: accept invitation: %w", ErrInvitationInvalid)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, is_admin, is_disabled, created_at, updated_at)
		VALUES (?, ?, 0, 0, ?, ?)
	`, username, passwordHash, now.Unix(), now.Unix())
	if err != nil {
		if isUniqueUsernameConflict(err) {
			return User{}, fmt.Errorf("store: accept invitation: %w", ErrConflict)
		}
		return User{}, fmt.Errorf("store: accept invitation: %w", err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("store: accept invitation: %w", err)
	}

	upd, err := tx.ExecContext(ctx, `
		UPDATE invitations SET used_by = ?, used_at = ? WHERE id = ? AND used_by IS NULL
	`, userID, now.Unix(), invID)
	if err != nil {
		return User{}, fmt.Errorf("store: accept invitation: %w", err)
	}
	n, err := upd.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("store: accept invitation: %w", err)
	}
	if n == 0 {
		return User{}, fmt.Errorf("store: accept invitation: %w", ErrInvitationInvalid)
	}

	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("store: accept invitation: %w", err)
	}
	return s.GetUserByID(ctx, userID)
}
