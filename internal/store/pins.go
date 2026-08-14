package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AddPin bookmarks entryID for later reading, copying its url/title at pin
// time. A no-op if entryID is already pinned.
func (s *Store) AddPin(ctx context.Context, entryID int64, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		INSERT INTO pins(entry_id, url, title, created_at)
		SELECT id, url, title, ? FROM entries WHERE id = ?
		ON CONFLICT(entry_id) DO NOTHING
	`, now.Unix(), entryID)
	if err != nil {
		return fmt.Errorf("store: add pin for entry %d: %w", entryID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: add pin for entry %d: %w", entryID, err)
	}
	if n == 0 {
		var exists bool
		if err := s.Read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id = ?)`, entryID).Scan(&exists); err != nil {
			return fmt.Errorf("store: add pin for entry %d: %w", entryID, err)
		}
		if !exists {
			return fmt.Errorf("store: add pin for entry %d: %w", entryID, ErrNotFound)
		}
	}
	return nil
}

// RemovePin unpins entryID. Returns ErrNotFound if it wasn't pinned.
func (s *Store) RemovePin(ctx context.Context, entryID int64) error {
	res, err := s.Write.ExecContext(ctx, `DELETE FROM pins WHERE entry_id = ?`, entryID)
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

// ListPins returns every pin, most recently pinned first.
func (s *Store) ListPins(ctx context.Context) ([]Pin, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT entry_id, url, title, created_at FROM pins ORDER BY created_at DESC
	`)
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
