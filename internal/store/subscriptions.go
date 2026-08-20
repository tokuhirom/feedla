package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpsertSubscription creates a subscription for (userID, feedID), or
// updates its folder/title if one already exists. folderID may be nil (no
// folder). A newly created subscription fans out into user_entry_state for
// every entry feedID already has (docs/multi-user-design.md's
// fan-out-on-write design), each entry's ignored flag computed against
// userID's own ignore_words -- mirroring UpsertEntries's insert-time
// computation for entries fetched after the subscription exists.
func (s *Store) UpsertSubscription(ctx context.Context, userID, feedID int64, folderID *int64, title string, now time.Time) error {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: upsert subscription for feed %d: begin tx: %w", feedID, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subscriptions(user_id, feed_id, folder_id, title, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, feed_id) DO UPDATE SET
			folder_id = excluded.folder_id,
			title     = excluded.title
	`, userID, feedID, folderID, title, now.Unix()); err != nil {
		return fmt.Errorf("store: upsert subscription for feed %d: %w", feedID, err)
	}

	// Fan out unconditionally rather than branching on whether this was a
	// fresh insert vs. an update to an existing subscription (SQLite's
	// RowsAffected for an ON CONFLICT ... DO UPDATE is driver behavior not
	// worth depending on here). Idempotent either way: the fan-out INSERT's
	// own ON CONFLICT DO NOTHING no-ops for entries this user already has a
	// user_entry_state row for, so re-running it on every re-subscribe just
	// costs a harmless full scan of the feed's entries.
	// created_at is copied from entries (each entry's own first-registered
	// time), not stamped with now -- otherwise subscribing to a feed with
	// an existing backlog would flood this user's "Today" group with
	// entries that were actually registered long ago.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_entry_state (user_id, entry_id, feed_id, published_at, created_at, read_at, ignored)
		SELECT ?, e.id, e.feed_id, e.published_at, e.created_at, NULL, EXISTS(
			SELECT 1 FROM ignore_words iw
			WHERE iw.user_id = ? AND (e.title LIKE '%' || iw.word || '%' OR e.body LIKE '%' || iw.word || '%')
		)
		FROM entries e WHERE e.feed_id = ?
		ON CONFLICT(user_id, entry_id) DO NOTHING
	`, userID, userID, feedID); err != nil {
		return fmt.Errorf("store: upsert subscription for feed %d: fan out: %w", feedID, err)
	}
	if err := refreshUnreadCount(ctx, tx, feedID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: upsert subscription for feed %d: commit: %w", feedID, err)
	}
	return nil
}

// Unsubscribe removes userID's subscription to feedID and their
// user_entry_state rows for it. feeds/entries/pins are left untouched --
// they're shared across users; deleting a feed once its last subscriber
// leaves is a separate GC concern (docs/multi-user-design.md defers it to
// Phase C). Returns ErrNotFound if userID has no such subscription.
func (s *Store) Unsubscribe(ctx context.Context, userID, feedID int64) error {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: unsubscribe feed %d: begin tx: %w", feedID, err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `DELETE FROM subscriptions WHERE user_id = ? AND feed_id = ?`, userID, feedID)
	if err != nil {
		return fmt.Errorf("store: unsubscribe feed %d: %w", feedID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: unsubscribe feed %d: %w", feedID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: unsubscribe feed %d: %w", feedID, ErrNotFound)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_entry_state WHERE user_id = ? AND feed_id = ?`, userID, feedID); err != nil {
		return fmt.Errorf("store: unsubscribe feed %d: clear entry state: %w", feedID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: unsubscribe feed %d: commit: %w", feedID, err)
	}
	return nil
}

// SubscribedFeedIDs returns the set of feed IDs userID subscribes to, for
// scoping data that's keyed by feed_id but has no per-user WHERE clause of
// its own (e.g. the crawler's in-memory internal-error log -- see
// docs/multi-user-design.md's "自分が購読している feed に限定" rule).
func (s *Store) SubscribedFeedIDs(ctx context.Context, userID int64) (map[int64]bool, error) {
	rows, err := s.Read.QueryContext(ctx, `SELECT feed_id FROM subscriptions WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: subscribed feed ids: %w", err)
	}
	defer rows.Close()

	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: subscribed feed ids: scan: %w", err)
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// CountSubscriptions returns how many subscriptions userID has, for
// enforcing the FR_QUOTA_MAX_SUBSCRIPTIONS limit.
func (s *Store) CountSubscriptions(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count subscriptions: %w", err)
	}
	return n, nil
}

// IsSubscribed reports whether userID subscribes to feedID, for scoping
// operations (like a manual refresh) to a caller's own subscriptions.
func (s *Store) IsSubscribed(ctx context.Context, userID, feedID int64) (bool, error) {
	var exists int
	err := s.Read.QueryRowContext(ctx,
		`SELECT 1 FROM subscriptions WHERE user_id = ? AND feed_id = ?`, userID, feedID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: is subscribed: %w", err)
	}
	return true, nil
}

// ListSubscriptions returns every subscription userID has.
func (s *Store) ListSubscriptions(ctx context.Context, userID int64) ([]Subscription, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT user_id, feed_id, folder_id, title, rating, unread_count, sort_order, created_at
		FROM subscriptions
		WHERE user_id = ?
		ORDER BY sort_order, feed_id
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var sub Subscription
		var title *string
		if err := rows.Scan(&sub.UserID, &sub.FeedID, &sub.FolderID, &title, &sub.Rating, &sub.UnreadCount, &sub.SortOrder, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan subscription: %w", err)
		}
		if title != nil {
			sub.Title = *title
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// scrapePrefixLike/scrapePrefixLen (pagewatch) and
// selectorPrefixLike/selectorPrefixLen must match crawler.ScrapePrefix
// ("pagewatch:") and crawler.SelectorPrefix ("selector:") — duplicated here
// rather than imported since store must not depend on crawler
// (internal/crawler -> internal/store is the only allowed direction).
const (
	scrapePrefixLike = `pagewatch:%`
	scrapePrefixLen  = len(`pagewatch:`)

	selectorPrefixLike = `selector:%`
	selectorPrefixLen  = len(`selector:`)
)

var subscriptionViewColumns = fmt.Sprintf(`
	s.feed_id,
	CASE
		WHEN f.feed_url LIKE '%[1]s' THEN substr(f.feed_url, %[2]d)
		WHEN f.feed_url LIKE '%[3]s' THEN substr(f.feed_url, %[4]d)
		ELSE f.feed_url
	END,
	CASE
		WHEN f.feed_url LIKE '%[1]s' THEN 'pagewatch'
		WHEN f.feed_url LIKE '%[3]s' THEN 'selector'
		ELSE 'feed'
	END,
	COALESCE(f.site_url, ''),
	CASE WHEN COALESCE(s.title, '') != '' THEN s.title ELSE f.title END,
	s.folder_id, s.rating, s.unread_count, f.last_status, f.error_count, f.last_error,
	f.last_fetched_at, f.next_fetch_at,
	(SELECT MAX(e.published_at) FROM entries e WHERE e.feed_id = s.feed_id),
	EXISTS(SELECT 1 FROM feed_fulltext ff WHERE ff.feed_id = s.feed_id)
`, scrapePrefixLike, scrapePrefixLen+1, selectorPrefixLike, selectorPrefixLen+1)

func scanSubscriptionView(row interface{ Scan(...any) error }) (SubscriptionView, error) {
	var v SubscriptionView
	err := row.Scan(&v.FeedID, &v.FeedURL, &v.Kind, &v.SiteURL, &v.Title,
		&v.FolderID, &v.Rating, &v.UnreadCount, &v.LastStatus, &v.ErrorCount, &v.LastError,
		&v.LastFetchedAt, &v.NextFetchAt, &v.LastEntryAt, &v.Fulltext)
	return v, err
}

// GetSubscriptionView returns a single subscription (userID's) joined with
// its feed's display/crawl metadata. Returns ErrNotFound if userID has no
// subscription for feedID.
func (s *Store) GetSubscriptionView(ctx context.Context, userID, feedID int64) (SubscriptionView, error) {
	v, err := scanSubscriptionView(s.Read.QueryRowContext(ctx, `
		SELECT `+subscriptionViewColumns+`
		FROM subscriptions s
		JOIN feeds f ON f.id = s.feed_id
		WHERE s.feed_id = ? AND s.user_id = ?
	`, feedID, userID))
	if err == sql.ErrNoRows {
		return SubscriptionView{}, fmt.Errorf("store: get subscription view for feed %d: %w", feedID, ErrNotFound)
	}
	if err != nil {
		return SubscriptionView{}, fmt.Errorf("store: get subscription view for feed %d: %w", feedID, err)
	}
	return v, nil
}

// ListSubscriptionViews returns every subscription userID has, joined with
// each feed's display/crawl metadata, in display order — the shape the
// subscription list API actually wants, so callers don't have to stitch two
// queries together themselves.
func (s *Store) ListSubscriptionViews(ctx context.Context, userID int64) ([]SubscriptionView, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT `+subscriptionViewColumns+`
		FROM subscriptions s
		JOIN feeds f ON f.id = s.feed_id
		WHERE s.user_id = ?
		ORDER BY s.sort_order, s.feed_id
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list subscription views: %w", err)
	}
	defer rows.Close()

	var views []SubscriptionView
	for rows.Next() {
		v, err := scanSubscriptionView(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan subscription view: %w", err)
		}
		views = append(views, v)
	}
	return views, rows.Err()
}

// SubscriptionPatch describes a partial update to a subscription. A nil
// field is left untouched; for FolderID specifically, a non-nil pointer to
// 0 clears the folder (moves the subscription to "no folder").
type SubscriptionPatch struct {
	Title    *string
	Rating   *int64
	FolderID *int64
}

// UpdateSubscription applies patch to userID's subscription for feedID.
// Returns ErrNotFound if userID has no subscription for that feed. Callers
// that accept patch.FolderID from the request must verify the folder
// belongs to userID themselves (e.g. via GetFolder) before calling this --
// this function trusts the folder id it's given.
func (s *Store) UpdateSubscription(ctx context.Context, userID, feedID int64, patch SubscriptionPatch) error {
	var sets []string
	var args []any
	if patch.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *patch.Title)
	}
	if patch.Rating != nil {
		sets = append(sets, "rating = ?")
		args = append(args, *patch.Rating)
	}
	if patch.FolderID != nil {
		if *patch.FolderID == 0 {
			sets = append(sets, "folder_id = NULL")
		} else {
			sets = append(sets, "folder_id = ?")
			args = append(args, *patch.FolderID)
		}
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, userID, feedID)

	res, err := s.Write.ExecContext(ctx,
		`UPDATE subscriptions SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND feed_id = ?`, args...)
	if err != nil {
		return fmt.Errorf("store: update subscription for feed %d: %w", feedID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update subscription for feed %d: %w", feedID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update subscription for feed %d: %w", feedID, ErrNotFound)
	}
	return nil
}
