package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// SetupSentinelHash is the password_hash value written by migration 0005
// for the bootstrap admin user (id=1). It's not a valid argon2id PHC
// string, so it can never be matched by a real password -- while a user
// has this hash, they can only get a real password via CompleteSetup.
const SetupSentinelHash = "!locked!"

// User is a feedla account. Phase A has exactly one (the admin created via
// the initial-setup flow); multi-user support is Phase B/C.
type User struct {
	ID                     int64  `json:"id"`
	Username               string `json:"username"`
	PasswordHash           string `json:"-"`
	IsAdmin                bool   `json:"is_admin"`
	IsDisabled             bool   `json:"is_disabled"`
	InstagramEmbedsEnabled bool   `json:"instagram_embeds_enabled"`
	CreatedAt              int64  `json:"created_at"`
	UpdatedAt              int64  `json:"updated_at"`
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.IsDisabled, &u.InstagramEmbedsEnabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, err
	}
	return u, nil
}

const userColumns = `id, username, password_hash, is_admin, is_disabled, instagram_embeds_enabled, created_at, updated_at`

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

// SetUserInstagramEmbedsEnabled updates userID's own
// instagram_embeds_enabled preference (see
// docs/adr/0001-third-party-embed-in-feed-content.md). Always scoped to
// userID -- callers pass the session's own user ID, never one read from
// request input, so there's no cross-user surface here to guard against.
func (s *Store) SetUserInstagramEmbedsEnabled(ctx context.Context, userID int64, enabled bool, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		UPDATE users SET instagram_embeds_enabled = ?, updated_at = ? WHERE id = ?
	`, enabled, now.Unix(), userID)
	if err != nil {
		return fmt.Errorf("store: set instagram_embeds_enabled for user %d: %w", userID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set instagram_embeds_enabled for user %d: %w", userID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: set instagram_embeds_enabled for user %d: %w", userID, ErrNotFound)
	}
	return nil
}

// isUniqueUsernameConflict reports whether err is a SQLite UNIQUE
// constraint violation. username is the only UNIQUE column on the users
// table, so any such violation from an INSERT against this table is a
// username collision.
func isUniqueUsernameConflict(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// ListUsers returns every account (including disabled ones), oldest first.
// Admin-only: callers must check the caller's own is_admin before exposing
// this.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.Read.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// CreateUser creates a new account with a real password from the start
// (unlike the bootstrap admin, which starts locked pending setup -- see
// SetupSentinelHash). Returns ErrConflict if username is already taken.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool, now time.Time) (User, error) {
	res, err := s.Write.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, is_admin, is_disabled, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?)
	`, username, passwordHash, isAdmin, now.Unix(), now.Unix())
	if err != nil {
		if isUniqueUsernameConflict(err) {
			return User{}, fmt.Errorf("store: create user %q: %w", username, ErrConflict)
		}
		return User{}, fmt.Errorf("store: create user %q: %w", username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("store: create user %q: %w", username, err)
	}
	return s.GetUserByID(ctx, id)
}

// ensureNotLastAdmin returns ErrLastAdmin if userID is the only remaining
// enabled admin, i.e. every other enabled admin has already been demoted
// or disabled. Must run inside tx alongside the mutation it's guarding, so
// concurrent admin-panel edits can't race past it.
func ensureNotLastAdmin(ctx context.Context, tx *sql.Tx, userID int64) error {
	var otherAdmins int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users WHERE is_admin = 1 AND is_disabled = 0 AND id != ?
	`, userID).Scan(&otherAdmins); err != nil {
		return fmt.Errorf("store: count other admins: %w", err)
	}
	if otherAdmins == 0 {
		return ErrLastAdmin
	}
	return nil
}

// SetUserAdmin grants or revokes admin. Revoking the last enabled admin
// fails with ErrLastAdmin, so the instance can never end up with nobody
// able to reach admin-only endpoints.
func (s *Store) SetUserAdmin(ctx context.Context, userID int64, isAdmin bool, now time.Time) error {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set admin for user %d: %w", userID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if !isAdmin {
		if err := ensureNotLastAdmin(ctx, tx, userID); err != nil {
			return err
		}
	}

	res, err := tx.ExecContext(ctx, `UPDATE users SET is_admin = ?, updated_at = ? WHERE id = ?`, isAdmin, now.Unix(), userID)
	if err != nil {
		return fmt.Errorf("store: set admin for user %d: %w", userID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set admin for user %d: %w", userID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: set admin for user %d: %w", userID, ErrNotFound)
	}
	return tx.Commit()
}

// SetUserDisabled enables or disables an account (login is refused for
// disabled accounts; see handleAuthLogin). Disabling the last enabled admin
// fails with ErrLastAdmin, for the same reason as SetUserAdmin.
func (s *Store) SetUserDisabled(ctx context.Context, userID int64, disabled bool, now time.Time) error {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set disabled for user %d: %w", userID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if disabled {
		var isAdmin bool
		if err := tx.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("store: set disabled for user %d: %w", userID, ErrNotFound)
			}
			return fmt.Errorf("store: set disabled for user %d: %w", userID, err)
		}
		if isAdmin {
			if err := ensureNotLastAdmin(ctx, tx, userID); err != nil {
				return err
			}
		}
	}

	res, err := tx.ExecContext(ctx, `UPDATE users SET is_disabled = ?, updated_at = ? WHERE id = ?`, disabled, now.Unix(), userID)
	if err != nil {
		return fmt.Errorf("store: set disabled for user %d: %w", userID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set disabled for user %d: %w", userID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: set disabled for user %d: %w", userID, ErrNotFound)
	}
	if disabled {
		// GetSessionByTokenHash already joins is_disabled = 0, so a
		// disabled account's sessions stop authenticating immediately;
		// this just reaps the now-dead rows instead of leaving them for
		// DeleteExpiredSessions to eventually clean up.
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
			return fmt.Errorf("store: delete sessions for disabled user %d: %w", userID, err)
		}
	}
	return tx.Commit()
}
