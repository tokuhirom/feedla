package store

import (
	"context"
	"fmt"
	"time"
)

// UpsertEntries writes a feed's parsed entries inside one transaction and
// returns how many were brand new (as opposed to updates to an
// already-known guid). New entries are inserted with read_at = NULL;
// existing entries have everything except published_at/read_at refreshed,
// so re-fetching never un-reads an entry or moves its position.
func (s *Store) UpsertEntries(ctx context.Context, feedID int64, entries []EntryInput, now time.Time) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: begin tx: %w", err)
	}
	defer tx.Rollback()

	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entries(feed_id, guid, url, title, author, body, body_hash, published_at, updated_at, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_id, guid) DO NOTHING
	`)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: prepare insert: %w", err)
	}
	defer func() { _ = insertStmt.Close() }()

	updateStmt, err := tx.PrepareContext(ctx, `
		UPDATE entries SET
			url = ?, title = ?, author = ?, body = ?, body_hash = ?, updated_at = ?, fetched_at = ?
		WHERE feed_id = ? AND guid = ?
	`)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: prepare update: %w", err)
	}
	defer func() { _ = updateStmt.Close() }()

	newCount := 0
	for _, e := range entries {
		res, err := insertStmt.ExecContext(ctx, feedID, e.GUID, e.URL, e.Title, e.Author, e.Body, e.BodyHash, e.PublishedAt, e.UpdatedAt, now.Unix())
		if err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: insert: %w", e.GUID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: rows affected: %w", e.GUID, err)
		}
		if n > 0 {
			newCount++
			continue
		}

		if _, err := updateStmt.ExecContext(ctx, e.URL, e.Title, e.Author, e.Body, e.BodyHash, e.UpdatedAt, now.Unix(), feedID, e.GUID); err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: update: %w", e.GUID, err)
		}
	}

	if newCount > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE subscriptions SET unread_count = (
				SELECT COUNT(*) FROM entries WHERE feed_id = ? AND read_at IS NULL
			) WHERE feed_id = ?
		`, feedID, feedID); err != nil {
			return 0, fmt.Errorf("store: refresh unread_count for feed %d: %w", feedID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: upsert entries: commit: %w", err)
	}
	return newCount, nil
}

// CountEntries returns how many entries exist for feedID. Test helper.
func (s *Store) CountEntries(ctx context.Context, feedID int64) (int, error) {
	var n int
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE feed_id = ?`, feedID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count entries: %w", err)
	}
	return n, nil
}
