package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// CreateScrapeSource registers a scrape method for feedID (typically a feed
// just created via UpsertFeed with a "pagewatch:"-prefixed feed_url) on
// createdBy's behalf. config may be nil, which is stored as the "no
// configuration" default '{}'.
func (s *Store) CreateScrapeSource(ctx context.Context, createdBy, feedID int64, kind, targetURL string, config json.RawMessage, now time.Time) (int64, error) {
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	var id int64
	err := s.Write.QueryRowContext(ctx, `
		INSERT INTO scrape_sources(feed_id, kind, target_url, config, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, feedID, kind, targetURL, string(config), createdBy, now.Unix(), now.Unix()).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: create scrape source for feed %d: %w", feedID, err)
	}
	return id, nil
}

const scrapeSourceColumns = `id, feed_id, kind, target_url, config, state, created_by, created_at, updated_at`

func scanScrapeSource(row interface{ Scan(...any) error }) (ScrapeSource, error) {
	var src ScrapeSource
	var config string
	var state sql.NullString
	err := row.Scan(&src.ID, &src.FeedID, &src.Kind, &src.TargetURL, &config, &state, &src.CreatedBy, &src.CreatedAt, &src.UpdatedAt)
	src.Config = json.RawMessage(config)
	if state.Valid {
		src.State = json.RawMessage(state.String)
	}
	return src, err
}

// GetScrapeSource returns a scrape source by its own id.
// CountScrapeSources returns how many scrape sources createdBy has
// authored, for enforcing the FR_QUOTA_MAX_SCRAPE_SOURCES limit.
func (s *Store) CountScrapeSources(ctx context.Context, createdBy int64) (int, error) {
	var n int
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM scrape_sources WHERE created_by = ?`, createdBy).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count scrape sources: %w", err)
	}
	return n, nil
}

func (s *Store) GetScrapeSource(ctx context.Context, id int64) (ScrapeSource, error) {
	src, err := scanScrapeSource(s.Read.QueryRowContext(ctx, `SELECT `+scrapeSourceColumns+` FROM scrape_sources WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return ScrapeSource{}, fmt.Errorf("store: get scrape source %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return ScrapeSource{}, fmt.Errorf("store: get scrape source %d: %w", id, err)
	}
	return src, nil
}

// GetScrapeSourceByFeedID returns the scrape source backing feedID. The
// crawler uses this to look up the pagewatch config/state for a
// "pagewatch:"-prefixed feed.
func (s *Store) GetScrapeSourceByFeedID(ctx context.Context, feedID int64) (ScrapeSource, error) {
	src, err := scanScrapeSource(s.Read.QueryRowContext(ctx, `SELECT `+scrapeSourceColumns+` FROM scrape_sources WHERE feed_id = ?`, feedID))
	if err == sql.ErrNoRows {
		return ScrapeSource{}, fmt.Errorf("store: get scrape source for feed %d: %w", feedID, ErrNotFound)
	}
	if err != nil {
		return ScrapeSource{}, fmt.Errorf("store: get scrape source for feed %d: %w", feedID, err)
	}
	return src, nil
}

// ListScrapeSources returns every scrape source, for the
// GET /api/v1/scrape_sources listing/backup endpoint.
func (s *Store) ListScrapeSources(ctx context.Context) ([]ScrapeSource, error) {
	rows, err := s.Read.QueryContext(ctx, `SELECT `+scrapeSourceColumns+` FROM scrape_sources ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list scrape sources: %w", err)
	}
	defer rows.Close()

	var out []ScrapeSource
	for rows.Next() {
		src, err := scanScrapeSource(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan scrape source: %w", err)
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// UpdateScrapeSourceConfig replaces a scrape source's config (e.g. editing
// ignore_patterns via PATCH /api/v1/scrape_sources/{id}). Returns
// ErrNotFound if id doesn't exist.
func (s *Store) UpdateScrapeSourceConfig(ctx context.Context, id int64, config json.RawMessage, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		UPDATE scrape_sources SET config = ?, updated_at = ? WHERE id = ?
	`, string(config), now.Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update scrape source %d config: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update scrape source %d config: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update scrape source %d config: %w", id, ErrNotFound)
	}
	return nil
}

// UpdateScrapeSourceTargetURL updates the scrape source's target_url after
// crawler follows a permanent redirect on feedID's underlying page, so the
// preview endpoint (which fetches target_url directly, not feeds.feed_url)
// doesn't keep hitting the pre-redirect URL indefinitely. See §14 of
// docs/feedless-site-subscription-selector.md.
func (s *Store) UpdateScrapeSourceTargetURL(ctx context.Context, feedID int64, targetURL string, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		UPDATE scrape_sources SET target_url = ?, updated_at = ? WHERE feed_id = ?
	`, targetURL, now.Unix(), feedID)
	if err != nil {
		return fmt.Errorf("store: update scrape source target_url for feed %d: %w", feedID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update scrape source target_url for feed %d: %w", feedID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update scrape source target_url for feed %d: %w", feedID, ErrNotFound)
	}
	return nil
}

// UpdateScrapeSourceState persists the opaque state extract.Extract
// returned for the scrape source backing feedID. extract.Result.State == nil
// means "leave the stored state as-is" (§7.3 of the pagewatch design, to
// avoid rewriting unchanged state on every poll) — callers must skip calling
// this at all in that case, not call it with an empty state.
func (s *Store) UpdateScrapeSourceState(ctx context.Context, feedID int64, state json.RawMessage, now time.Time) error {
	res, err := s.Write.ExecContext(ctx, `
		UPDATE scrape_sources SET state = ?, updated_at = ? WHERE feed_id = ?
	`, string(state), now.Unix(), feedID)
	if err != nil {
		return fmt.Errorf("store: update scrape source state for feed %d: %w", feedID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update scrape source state for feed %d: %w", feedID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update scrape source state for feed %d: %w", feedID, ErrNotFound)
	}
	return nil
}
