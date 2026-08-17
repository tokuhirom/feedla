package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AddPin bookmarks entryID for later reading on userID's behalf, copying its
// url/title at pin time. A no-op if userID already pinned it. Returns
// ErrNotFound if userID doesn't subscribe to entryID's feed (checked via
// user_entry_state, which fan-out-on-write guarantees a row for iff
// subscribed) -- this also covers "entry doesn't exist at all".
func (s *Store) AddPin(ctx context.Context, userID, entryID int64, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		INSERT INTO pins(user_id, entry_id, url, title, created_at)
		SELECT ?, id, url, title, ? FROM entries WHERE id = ?
		ON CONFLICT(user_id, entry_id) DO NOTHING
	`, userID, now.Unix(), entryID)
	if err != nil {
		return fmt.Errorf("store: add pin for entry %d: %w", entryID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: add pin for entry %d: %w", entryID, err)
	}
	if n == 0 {
		var exists bool
		if err := s.Read.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM user_entry_state WHERE user_id = ? AND entry_id = ?)`,
			userID, entryID).Scan(&exists); err != nil {
			return fmt.Errorf("store: add pin for entry %d: %w", entryID, err)
		}
		if !exists {
			return fmt.Errorf("store: add pin for entry %d: %w", entryID, ErrNotFound)
		}
	}
	return nil
}

// RemovePin unpins entryID on userID's behalf. Returns ErrNotFound if it
// wasn't pinned.
func (s *Store) RemovePin(ctx context.Context, userID, entryID int64) error {
	res, err := s.Write.ExecContext(ctx, `DELETE FROM pins WHERE user_id = ? AND entry_id = ?`, userID, entryID)
	if err != nil {
		return fmt.Errorf("store: remove pin for entry %d: %w", entryID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: remove pin for entry %d: %w", entryID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: remove pin for entry %d: %w", entryID, ErrNotFound)
	}
	return nil
}

// CountPins returns how many pins userID has, for enforcing the
// FR_QUOTA_MAX_PINS limit.
func (s *Store) CountPins(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM pins WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count pins: %w", err)
	}
	return n, nil
}

// ListPins returns every pin userID has made, most recently pinned first.
func (s *Store) ListPins(ctx context.Context, userID int64) ([]Pin, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT entry_id, url, title, created_at FROM pins WHERE user_id = ? ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list pins: %w", err)
	}
	defer rows.Close()

	var pins []Pin
	for rows.Next() {
		var p Pin
		if err := rows.Scan(&p.EntryID, &p.URL, &p.Title, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan pin: %w", err)
		}
		pins = append(pins, p)
	}
	return pins, rows.Err()
}

// FindEntryByURL resolves url to an entry id, for LDR-compatible pin
// endpoints that only carry a link. If several entries share the url, the
// most recently published one wins. Returns ErrNotFound if none match.
// Entries have no user_id (feeds/entries are shared across users), so this
// stays global; the caller's AddPin/RemovePin call is where per-user
// authorization actually happens.
func (s *Store) FindEntryByURL(ctx context.Context, url string) (int64, error) {
	var id int64
	err := s.Read.QueryRowContext(ctx, `
		SELECT id FROM entries WHERE url = ? ORDER BY published_at DESC, id DESC LIMIT 1
	`, url).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("store: find entry by url %q: %w", url, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("store: find entry by url %q: %w", url, err)
	}
	return id, nil
}
