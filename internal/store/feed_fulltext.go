package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FeedFulltext records that feedID's entries should have their body
// replaced by content extracted from the entry's own link
// (internal/fulltext), for feeds whose entries carry only a short summary.
// Unrelated to ScrapeSource (feedless/pagewatch): this augments a real
// feed's real entries rather than synthesizing a feed from a scraped page.
type FeedFulltext struct {
	FeedID    int64
	CreatedBy int64
	CreatedAt int64
}

// EnableFeedFulltext turns on fulltext extraction for feedID. Idempotent:
// re-enabling an already-enabled feed leaves the original CreatedBy/CreatedAt
// untouched.
func (s *Store) EnableFeedFulltext(ctx context.Context, feedID, createdBy int64, now time.Time) error {
	if _, err := s.Write.ExecContext(ctx, `
		INSERT INTO feed_fulltext(feed_id, created_by, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(feed_id) DO NOTHING
	`, feedID, createdBy, now.Unix()); err != nil {
		return fmt.Errorf("store: enable feed fulltext for feed %d: %w", feedID, err)
	}
	return nil
}

// DisableFeedFulltext turns off fulltext extraction for feedID. Not an
// error if it was already disabled.
func (s *Store) DisableFeedFulltext(ctx context.Context, feedID int64) error {
	if _, err := s.Write.ExecContext(ctx, `DELETE FROM feed_fulltext WHERE feed_id = ?`, feedID); err != nil {
		return fmt.Errorf("store: disable feed fulltext for feed %d: %w", feedID, err)
	}
	return nil
}

// GetFeedFulltext returns feedID's fulltext row, or ErrNotFound if fulltext
// extraction isn't enabled for it. The crawler calls this once per crawl of
// a non-scrape feed; ErrNotFound is the common case and is cheap (an
// indexed point lookup on the primary key).
func (s *Store) GetFeedFulltext(ctx context.Context, feedID int64) (FeedFulltext, error) {
	var f FeedFulltext
	err := s.Read.QueryRowContext(ctx, `
		SELECT feed_id, created_by, created_at FROM feed_fulltext WHERE feed_id = ?
	`, feedID).Scan(&f.FeedID, &f.CreatedBy, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return FeedFulltext{}, fmt.Errorf("store: get feed fulltext for feed %d: %w", feedID, ErrNotFound)
	}
	if err != nil {
		return FeedFulltext{}, fmt.Errorf("store: get feed fulltext for feed %d: %w", feedID, err)
	}
	return f, nil
}
