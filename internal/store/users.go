package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SetupSentinelHash is the password_hash value written by migration 0005
// for the bootstrap admin user (id=1). It's not a valid argon2id PHC
// string, so it can never be matched by a real password -- while a user
// has this hash, they can only get a real password via CompleteSetup.
const SetupSentinelHash = "!locked!"

// User is a feedla account. Phase A has exactly one (the admin created via
// the initial-setup flow); multi-user support is Phase B/C.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	IsAdmin      bool   `json:"is_admin"`
	IsDisabled   bool   `json:"is_disabled"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.IsDisabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, err
	}
	return u, nil
}

const userColumns = `id, username, password_hash, is_admin, is_disabled, created_at, updated_at`

// GetUserByUsername returns ErrNotFound if no such user exists.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	u, err := scanUser(s.Read.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ? COLLATE NOCASE`, username))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("store: get user %q: %w", username, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user %q: %w", username, err)
	}
	return u, nil
}

// GetUserByID returns ErrNotFound if no such user exists.
func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.Read.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("store: get user %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user %d: %w", id, err)
	}
	return u, nil
}

// IsSetupPending reports whether userID still has the sentinel password
// set by the auth migration, i.e. hasn't completed initial setup yet.
func (s *Store) IsSetupPending(ctx context.Context, userID int64) (bool, error) {
	u, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return u.PasswordHash == SetupSentinelHash, nil
}

// CompleteSetup sets userID's username and password, but only while it
// still has the sentinel hash -- once a real password is set, this can
// never be called again, so there's no window after initial setup where
// the admin account can be silently reset via this path.
func (s *Store) CompleteSetup(ctx context.Context, userID int64, username, passwordHash string, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		UPDATE users SET username = ?, password_hash = ?, updated_at = ?
		WHERE id = ? AND password_hash = ?
	`, username, passwordHash, now.Unix(), userID, SetupSentinelHash)
	if err != nil {
		return fmt.Errorf("store: complete setup for user %d: %w", userID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: complete setup for user %d: %w", userID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: complete setup for user %d: %w", userID, ErrNotFound)
	}
	return nil
}

// UpdateUserPassword sets userID's password hash unconditionally. Used by
// the "change password" flow (which already verified the current
// password), unlike CompleteSetup which only fires once from the sentinel
// state.
func (s *Store) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?
	`, passwordHash, now.Unix(), userID)
	if err != nil {
		return fmt.Errorf("store: update password for user %d: %w", userID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update password for user %d: %w", userID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update password for user %d: %w", userID, ErrNotFound)
	}
	return nil
}
