package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a logged-in browser session. Only its SHA-256 token hash is
// ever persisted (see internal/auth.HashToken) -- the raw token lives only
// in the session cookie, so a leaked DB/backup can't be replayed.
type Session struct {
	ID         int64
	UserID     int64
	CreatedAt  int64
	LastSeenAt int64
	ExpiresAt  int64
}

// SessionWithUser is a validated session joined with its owner, as needed
// by the auth middleware on every authenticated request.
type SessionWithUser struct {
	Session
	User User
}

// CreateSession inserts a new session row for userID and returns it.
func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash []byte, now time.Time, expiresAt time.Time) (Session, error) {
	res, err := s.Write.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`, tokenHash, userID, now.Unix(), now.Unix(), expiresAt.Unix())
	if err != nil {
		return Session{}, fmt.Errorf("store: create session for user %d: %w", userID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Session{}, fmt.Errorf("store: create session for user %d: %w", userID, err)
	}
	return Session{ID: id, UserID: userID, CreatedAt: now.Unix(), LastSeenAt: now.Unix(), ExpiresAt: expiresAt.Unix()}, nil
}

// GetSessionByTokenHash returns ErrNotFound if no session matches, or if
// its owning user is disabled (a disabled user's existing sessions stop
// working immediately, without needing to enumerate and delete them).
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (SessionWithUser, error) {
	row := s.Read.QueryRowContext(ctx, `
		SELECT s.id, s.user_id, s.created_at, s.last_seen_at, s.expires_at,
		       u.id, u.username, u.password_hash, u.is_admin, u.is_disabled, u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND u.is_disabled = 0
	`, tokenHash)

	var sw SessionWithUser
	err := row.Scan(
		&sw.ID, &sw.UserID, &sw.CreatedAt, &sw.LastSeenAt, &sw.ExpiresAt,
		&sw.User.ID, &sw.User.Username, &sw.User.PasswordHash, &sw.User.IsAdmin, &sw.User.IsDisabled, &sw.User.CreatedAt, &sw.User.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionWithUser{}, fmt.Errorf("store: get session: %w", ErrNotFound)
	}
	if err != nil {
		return SessionWithUser{}, fmt.Errorf("store: get session: %w", err)
	}
	return sw, nil
}

// TouchSession updates last_seen_at. Callers should throttle this (e.g. at
// most once/hour per docs/multi-user-design.md) rather than calling it on
// every request, to keep the write-conn load down.
func (s *Store) TouchSession(ctx context.Context, id int64, now time.Time) error {
	if _, err := s.Write.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
		return fmt.Errorf("store: touch session %d: %w", id, err)
	}
	return nil
}

// DeleteSession removes a single session by its token hash (logout).
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := s.Write.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return nil
}

// DeleteAllSessionsForUser logs a user out of every device -- used after a
// password change and available as an explicit "log out everywhere" action.
func (s *Store) DeleteAllSessionsForUser(ctx context.Context, userID int64) error {
	if _, err := s.Write.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete sessions for user %d: %w", userID, err)
	}
	return nil
}

// DeleteExpiredSessions removes every session past its absolute expiry,
// for the daily maintenance job. Returns the number of rows removed.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.Write.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now.Unix())
	if err != nil {
		return 0, fmt.Errorf("store: delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete expired sessions: %w", err)
	}
	return n, nil
}
