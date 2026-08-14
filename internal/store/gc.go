package store

import (
	"context"
	"fmt"
	"time"
)

// DeleteOldReadEntries removes read, unpinned entries whose fetched_at is
// older than before. It returns the number of rows deleted. Uses
// idx_entries_gc (fetched_at WHERE read_at IS NOT NULL).
func (s *Store) DeleteOldReadEntries(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Write.ExecContext(ctx, `
		DELETE FROM entries
		WHERE read_at IS NOT NULL
		  AND fetched_at < ?
		  AND id NOT IN (SELECT entry_id FROM pins)
	`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("store: delete old read entries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete old read entries: rows affected: %w", err)
	}
	return n, nil
}

// TrimExcessEntries deletes read, unpinned entries beyond the newest
// perFeedLimit entries in each feed (ranked by published_at, id). Feeds
// with fewer than perFeedLimit entries are untouched. It returns the number
// of rows deleted.
func (s *Store) TrimExcessEntries(ctx context.Context, perFeedLimit int) (int64, error) {
	res, err := s.Write.ExecContext(ctx, `
		DELETE FROM entries
		WHERE read_at IS NOT NULL
		  AND id NOT IN (SELECT entry_id FROM pins)
		  AND id IN (
		    SELECT id FROM (
		      SELECT id, ROW_NUMBER() OVER (
		        PARTITION BY feed_id ORDER BY published_at DESC, id DESC
		      ) AS rn
		      FROM entries
		    )
		    WHERE rn > ?
		  )
	`, perFeedLimit)
	if err != nil {
		return 0, fmt.Errorf("store: trim excess entries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: trim excess entries: rows affected: %w", err)
	}
	return n, nil
}

// Optimize runs PRAGMA optimize, which lets SQLite refresh query planner
// statistics for tables that have changed significantly. Intended to be
// called periodically (README recommends daily), not per-query.
func (s *Store) Optimize(ctx context.Context) error {
	if _, err := s.Write.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return fmt.Errorf("store: optimize: %w", err)
	}
	return nil
}
