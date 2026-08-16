package store

import (
	"context"
	"fmt"
	"time"
)

// DeleteOldReadEntries removes entries whose fetched_at is older than
// before, that nobody has unread and nobody has pinned. It returns the
// number of rows deleted, and decrements unread_count for any subscriber
// whose still-unread rows are swept up in the deletion (shouldn't happen in
// practice since the NOT EXISTS guard excludes anyone-unread entries, but
// kept for symmetry with TrimExcessEntries below).
func (s *Store) DeleteOldReadEntries(ctx context.Context, before time.Time) (int64, error) {
	return s.deleteEntriesAndRefresh(ctx, `
		DELETE FROM entries WHERE id IN (
			SELECT e.id FROM entries e
			WHERE e.fetched_at < ?
			  AND NOT EXISTS (SELECT 1 FROM user_entry_state ues WHERE ues.entry_id = e.id AND ues.read_at IS NULL)
			  AND NOT EXISTS (SELECT 1 FROM pins p WHERE p.entry_id = e.id)
		)
		RETURNING feed_id
	`, before.Unix())
}

// TrimExcessEntries deletes unpinned entries beyond the newest
// perFeedLimit entries in each feed (ranked by published_at, id). Feeds
// with fewer than perFeedLimit entries are untouched. Unlike
// DeleteOldReadEntries, this cap applies even to entries some subscriber
// still has unread (docs/multi-user-design.md's retention section: the
// per-feed count cap is a hard limit, not conditional on every subscriber
// having read it -- otherwise one inactive subscriber pins the DB's growth
// with no upper bound). It returns the number of rows deleted, and
// decrements unread_count for any subscriber whose unread entries were
// swept away.
func (s *Store) TrimExcessEntries(ctx context.Context, perFeedLimit int) (int64, error) {
	return s.deleteEntriesAndRefresh(ctx, `
		DELETE FROM entries WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY feed_id ORDER BY published_at DESC, id DESC
				) AS rn
				FROM entries
			)
			WHERE rn > ?
		) AND id NOT IN (SELECT entry_id FROM pins)
		RETURNING feed_id
	`, perFeedLimit)
}

// deleteEntriesAndRefresh runs a DELETE ... RETURNING feed_id query and
// refreshes unread_count for every distinct feed it touched, since a
// deleted entry that was some subscriber's unread row leaves their cached
// count stale otherwise.
func (s *Store) deleteEntriesAndRefresh(ctx context.Context, query string, args ...any) (int64, error) {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: delete entries: begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: delete entries: %w", err)
	}
	seen := make(map[int64]bool)
	var feedIDs []int64
	var n int64
	for rows.Next() {
		var feedID int64
		if err := rows.Scan(&feedID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("store: delete entries: scan: %w", err)
		}
		n++
		if !seen[feedID] {
			seen[feedID] = true
			feedIDs = append(feedIDs, feedID)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("store: delete entries: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("store: delete entries: %w", err)
	}

	for _, feedID := range feedIDs {
		if err := refreshUnreadCount(ctx, tx, feedID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: delete entries: commit: %w", err)
	}
	return n, nil
}

// Optimize runs PRAGMA optimize, which lets SQLite refresh query planner
// statistics for tables that have changed significantly. Intended to be
// called periodically (docs/DESIGN.md recommends daily), not per-query.
func (s *Store) Optimize(ctx context.Context) error {
	if _, err := s.Write.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return fmt.Errorf("store: optimize: %w", err)
	}
	return nil
}
