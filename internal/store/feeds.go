package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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

const feedColumns = `
	id, feed_url, site_url, title, description, favicon_url,
	etag, last_modified, body_hash,
	fetch_interval_sec, next_fetch_at, last_fetched_at, last_success_at, last_status,
	error_count, last_error, created_at, updated_at
`

func scanFeed(row interface{ Scan(...any) error }) (Feed, error) {
	var f Feed
	var siteURL, favicon sql.NullString
	err := row.Scan(
		&f.ID, &f.FeedURL, &siteURL, &f.Title, &f.Description, &favicon,
		&f.ETag, &f.LastModified, &f.BodyHash,
		&f.FetchIntervalSec, &f.NextFetchAt, &f.LastFetchedAt, &f.LastSuccessAt, &f.LastStatus,
		&f.ErrorCount, &f.LastError, &f.CreatedAt, &f.UpdatedAt,
	)
	f.SiteURL = siteURL.String
	f.FaviconURL = favicon.String
	return f, err
}

// GetFeed returns a single feed by id.
func (s *Store) GetFeed(ctx context.Context, id int64) (Feed, error) {
	f, err := scanFeed(s.Read.QueryRowContext(ctx, `SELECT `+feedColumns+` FROM feeds WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Feed{}, fmt.Errorf("store: get feed %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Feed{}, fmt.Errorf("store: get feed %d: %w", id, err)
	}
	return f, nil
}

// DeleteFeed removes a feed and (via ON DELETE CASCADE) its subscription,
// entries and pins — the store's notion of "unsubscribe".
func (s *Store) DeleteFeed(ctx context.Context, id int64) error {
	res, err := s.Write.ExecContext(ctx, `DELETE FROM feeds WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete feed %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete feed %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete feed %d: %w", id, ErrNotFound)
	}
	return nil
}

// ListFeeds returns every known feed.
func (s *Store) ListFeeds(ctx context.Context) ([]Feed, error) {
	rows, err := s.Read.QueryContext(ctx, `SELECT `+feedColumns+` FROM feeds ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list feeds: %w", err)
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan feed: %w", err)
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// ListDueFeeds returns feeds whose next_fetch_at has passed, oldest-due
// first, capped at limit. Feeds with error_count >= 20 are excluded (they've
// been flagged as stopped, per the idx_feeds_next_fetch partial index).
func (s *Store) ListDueFeeds(ctx context.Context, now time.Time, limit int) ([]Feed, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT `+feedColumns+`
		FROM feeds
		WHERE next_fetch_at <= ? AND error_count < 20
		ORDER BY next_fetch_at
		LIMIT ?
	`, now.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("store: list due feeds: %w", err)
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan feed: %w", err)
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// ClaimDueFeeds atomically selects feeds whose next_fetch_at has passed
// (oldest-due first, capped at limit, error_count >= 20 excluded) and pushes
// their next_fetch_at out by their own fetch_interval_sec before returning
// them. This "claim" happens in the same statement as the selection so a
// scheduler tick can never dispatch the same feed twice while a slow fetch
// from an earlier tick is still in flight — UpdateFeedAfterFetch overwrites
// this provisional value with the real outcome-based one once the fetch
// completes.
func (s *Store) ClaimDueFeeds(ctx context.Context, now time.Time, limit int) ([]Feed, error) {
	rows, err := s.Write.QueryContext(ctx, `
		WITH due AS (
			SELECT id FROM feeds
			WHERE next_fetch_at <= ?1 AND error_count < 20
			ORDER BY next_fetch_at
			LIMIT ?2
		)
		UPDATE feeds
		SET next_fetch_at = ?1 + fetch_interval_sec
		WHERE id IN (SELECT id FROM due)
		RETURNING `+feedColumns+`
	`, now.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("store: claim due feeds: %w", err)
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan claimed feed: %w", err)
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// FeedFetchUpdate carries the outcome of one crawl attempt back into the
// feeds row. Title/SiteURL only overwrite an empty existing value, mirroring
// UpsertFeed's "don't clobber user-visible fields" rule.
type FeedFetchUpdate struct {
	Title            string
	SiteURL          string
	NewFeedURL       *string // set when the crawler followed a 301/308 to a new location
	ETag             *string
	LastModified     *string
	BodyHash         []byte
	FetchIntervalSec int64
	NextFetchAt      int64
	LastStatus       int
	Success          bool // if true, last_success_at is set to now and error_count resets to 0
	Gone             bool // 410 Gone: force error_count to the idx_feeds_next_fetch exclusion threshold so it stops being crawled
	LastError        *string
}

// UpdateFeedAfterFetch records a fetch attempt's outcome, including the
// crawler's adaptively-computed fetch_interval_sec/next_fetch_at.
//
// If the crawler followed a permanent redirect to a URL that's already used
// by a different feed row, applying NewFeedURL would violate feed_url's
// UNIQUE constraint. Since the redirect target doesn't change between runs,
// failing outright would make this feed fail the same way on every future
// tick forever. Instead, the feed_url change is dropped (the rest of the
// update still applies) and last_error notes the conflict so it's visible.
func (s *Store) UpdateFeedAfterFetch(ctx context.Context, feedID int64, u FeedFetchUpdate, now time.Time) error {
	err := s.execUpdateFeedAfterFetch(ctx, feedID, u, now)
	if err != nil && u.NewFeedURL != nil && isUniqueFeedURLConflict(err) {
		conflictURL := *u.NewFeedURL
		u.NewFeedURL = nil
		msg := fmt.Sprintf("permanent redirect target %q already used by another feed; feed_url left unchanged", conflictURL)
		u.LastError = &msg
		err = s.execUpdateFeedAfterFetch(ctx, feedID, u, now)
	}
	if err != nil {
		return fmt.Errorf("store: update feed %d after fetch: %w", feedID, err)
	}
	return nil
}

func (s *Store) execUpdateFeedAfterFetch(ctx context.Context, feedID int64, u FeedFetchUpdate, now time.Time) error {
	var lastSuccessAt *int64
	errorCount := "error_count + 1"
	switch {
	case u.Gone:
		errorCount = "20"
	case u.Success:
		ts := now.Unix()
		lastSuccessAt = &ts
		errorCount = "0"
	}

	_, err := s.Write.ExecContext(ctx, `
		UPDATE feeds SET
			title              = CASE WHEN title = '' THEN COALESCE(NULLIF(?, ''), '') ELSE title END,
			site_url           = CASE WHEN COALESCE(site_url, '') = '' THEN NULLIF(?, '') ELSE site_url END,
			feed_url           = COALESCE(?, feed_url),
			etag               = COALESCE(?, etag),
			last_modified      = COALESCE(?, last_modified),
			body_hash          = COALESCE(?, body_hash),
			fetch_interval_sec = ?,
			next_fetch_at      = ?,
			last_fetched_at    = ?,
			last_success_at    = COALESCE(?, last_success_at),
			last_status        = ?,
			error_count        = `+errorCount+`,
			last_error         = ?,
			updated_at         = ?
		WHERE id = ?
	`, u.Title, u.SiteURL, u.NewFeedURL, u.ETag, u.LastModified, u.BodyHash,
		u.FetchIntervalSec, u.NextFetchAt, now.Unix(), lastSuccessAt, u.LastStatus, u.LastError, now.Unix(), feedID)
	return err
}

// isUniqueFeedURLConflict reports whether err is a SQLite UNIQUE constraint
// violation. feed_url is the only UNIQUE column on the feeds table, so any
// such violation from an UPDATE against this table is a feed_url collision.
func isUniqueFeedURLConflict(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
