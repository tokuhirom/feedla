package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// GetFeedBoilerplate returns feedID's stored boilerplate-removal state
// (internal/fulltext/boilerplate), or ErrNotFound when the feed has none
// yet. Callers treat ErrNotFound as "start learning from scratch" rather
// than as a failure.
func (s *Store) GetFeedBoilerplate(ctx context.Context, feedID int64) (json.RawMessage, error) {
	var state []byte
	err := s.Read.QueryRowContext(ctx, `
		SELECT state FROM feed_boilerplate WHERE feed_id = ?
	`, feedID).Scan(&state)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("store: get feed boilerplate for feed %d: %w", feedID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get feed boilerplate for feed %d: %w", feedID, err)
	}
	return state, nil
}

// PutFeedBoilerplate stores feedID's boilerplate state, replacing any
// previous one.
func (s *Store) PutFeedBoilerplate(ctx context.Context, feedID int64, state json.RawMessage, now time.Time) error {
	if _, err := s.Write.ExecContext(ctx, `
		INSERT INTO feed_boilerplate(feed_id, state, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(feed_id) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at
	`, feedID, string(state), now.Unix()); err != nil {
		return fmt.Errorf("store: put feed boilerplate for feed %d: %w", feedID, err)
	}
	return nil
}

// DeleteFeedBoilerplate discards feedID's learned state. Not an error if
// there was none.
func (s *Store) DeleteFeedBoilerplate(ctx context.Context, feedID int64) error {
	if _, err := s.Write.ExecContext(ctx, `DELETE FROM feed_boilerplate WHERE feed_id = ?`, feedID); err != nil {
		return fmt.Errorf("store: delete feed boilerplate for feed %d: %w", feedID, err)
	}
	return nil
}
