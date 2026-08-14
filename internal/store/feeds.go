package store

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// UpsertFeed inserts a feed if feedURL is new, or leaves the existing row
// mostly untouched (only site_url/updated_at refresh) if it's already known.
// A random jitter within [0, defaultIntervalSec) is added to next_fetch_at
// on first insert so that newly imported feeds don't all become due at once.
func (s *Store) UpsertFeed(ctx context.Context, feedURL, siteURL, title string, defaultIntervalSec int64, now time.Time) (int64, error) {
	if defaultIntervalSec <= 0 {
		defaultIntervalSec = 1800
	}
	jitter := rand.Int63n(defaultIntervalSec)
	nextFetchAt := now.Unix() + jitter

	var id int64
	err := s.Write.QueryRowContext(ctx, `
		INSERT INTO feeds(feed_url, site_url, title, fetch_interval_sec, next_fetch_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_url) DO UPDATE SET
			site_url   = CASE WHEN excluded.site_url != '' THEN excluded.site_url ELSE feeds.site_url END,
			title      = CASE WHEN feeds.title = '' THEN excluded.title ELSE feeds.title END,
			updated_at = excluded.updated_at
		RETURNING id
	`, feedURL, siteURL, title, defaultIntervalSec, nextFetchAt, now.Unix(), now.Unix()).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: upsert feed %q: %w", feedURL, err)
	}
	return id, nil
}

// ListFeeds returns every known feed.
func (s *Store) ListFeeds(ctx context.Context) ([]Feed, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT id, feed_url, site_url, title, description, fetch_interval_sec, next_fetch_at, created_at, updated_at
		FROM feeds
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list feeds: %w", err)
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var f Feed
		if err := rows.Scan(&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description, &f.FetchIntervalSec, &f.NextFetchAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan feed: %w", err)
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}
