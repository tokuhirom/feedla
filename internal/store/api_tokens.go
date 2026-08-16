package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// APIToken is a long-lived credential for non-browser clients (Fastladder-
// compatible readers etc.). Only its SHA-256 hash is persisted.
type APIToken struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
}

// APITokenWithUser is a validated token joined with its owner, as needed
// by the auth middleware.
type APITokenWithUser struct {
	APIToken
	User User
}

// CreateAPIToken inserts a new token row for userID.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, label string, tokenHash []byte, now time.Time) (APIToken, error) {
	res, err := s.Write.ExecContext(ctx, `
		INSERT INTO api_tokens (token_hash, user_id, label, created_at) VALUES (?, ?, ?, ?)
	`, tokenHash, userID, label, now.Unix())
	if err != nil {
		return APIToken{}, fmt.Errorf("store: create api token for user %d: %w", userID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return APIToken{}, fmt.Errorf("store: create api token for user %d: %w", userID, err)
	}
	return APIToken{ID: id, UserID: userID, Label: label, CreatedAt: now.Unix()}, nil
}

// GetAPITokenByHash returns ErrNotFound if no token matches, or if its
// owning user is disabled.
func (s *Store) GetAPITokenByHash(ctx context.Context, tokenHash []byte) (APITokenWithUser, error) {
	row := s.Read.QueryRowContext(ctx, `
		SELECT t.id, t.user_id, t.label, t.created_at, t.last_used_at,
		       u.id, u.username, u.password_hash, u.is_admin, u.is_disabled, u.created_at, u.updated_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND u.is_disabled = 0
	`, tokenHash)

	var tw APITokenWithUser
	err := row.Scan(
		&tw.ID, &tw.UserID, &tw.Label, &tw.CreatedAt, &tw.LastUsedAt,
		&tw.User.ID, &tw.User.Username, &tw.User.PasswordHash, &tw.User.IsAdmin, &tw.User.IsDisabled, &tw.User.CreatedAt, &tw.User.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return APITokenWithUser{}, fmt.Errorf("store: get api token: %w", ErrNotFound)
	}
	if err != nil {
		return APITokenWithUser{}, fmt.Errorf("store: get api token: %w", err)
	}
	return tw, nil
}

// ListAPITokensForUser returns userID's tokens, newest first. Hashes are
// never returned (APIToken has no such field).
func (s *Store) ListAPITokensForUser(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT id, user_id, label, created_at, last_used_at
		FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list api tokens for user %d: %w", userID, err)
	}
	defer rows.Close()

	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Label, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, fmt.Errorf("store: list api tokens for user %d: %w", userID, err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes id, scoped to userID so a user can only delete
// their own tokens. Returns ErrNotFound if id doesn't exist or belongs to
// someone else.
func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	res, err := s.Write.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("store: delete api token %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete api token %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete api token %d: %w", id, ErrNotFound)
	}
	return nil
}

// TouchAPITokenLastUsed updates last_used_at. Callers should throttle this
// like TouchSession to avoid a write per request.
func (s *Store) TouchAPITokenLastUsed(ctx context.Context, id int64, now time.Time) error {
	if _, err := s.Write.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
		return fmt.Errorf("store: touch api token %d: %w", id, err)
	}
	return nil
}
