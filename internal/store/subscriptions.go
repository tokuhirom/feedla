package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UpsertSubscription creates a subscription for feedID, or updates its
// folder/title if one already exists. folderID may be nil (no folder).
func (s *Store) UpsertSubscription(ctx context.Context, feedID int64, folderID *int64, title string, now time.Time) error {
	_, err := s.Write.ExecContext(ctx, `
		INSERT INTO subscriptions(feed_id, folder_id, title, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(feed_id) DO UPDATE SET
			folder_id = excluded.folder_id,
			title     = excluded.title
	`, feedID, folderID, title, now.Unix())
	if err != nil {
		return fmt.Errorf("store: upsert subscription for feed %d: %w", feedID, err)
	}
	return nil
}

// ListSubscriptions returns every subscription.
func (s *Store) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT feed_id, folder_id, title, rating, unread_count, sort_order, created_at
		FROM subscriptions
		ORDER BY sort_order, feed_id
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var sub Subscription
		var title *string
		if err := rows.Scan(&sub.FeedID, &sub.FolderID, &title, &sub.Rating, &sub.UnreadCount, &sub.SortOrder, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan subscription: %w", err)
		}
		if title != nil {
			sub.Title = *title
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

const subscriptionViewColumns = `
	s.feed_id, f.feed_url, COALESCE(f.site_url, ''),
	CASE WHEN COALESCE(s.title, '') != '' THEN s.title ELSE f.title END,
	s.folder_id, s.rating, s.unread_count, f.last_status, f.error_count, f.last_error
`

func scanSubscriptionView(row interface{ Scan(...any) error }) (SubscriptionView, error) {
	var v SubscriptionView
	err := row.Scan(&v.FeedID, &v.FeedURL, &v.SiteURL, &v.Title,
		&v.FolderID, &v.Rating, &v.UnreadCount, &v.LastStatus, &v.ErrorCount, &v.LastError)
	return v, err
}

// GetSubscriptionView returns a single subscription joined with its feed's
// display/crawl metadata. Returns ErrNotFound if feedID has no subscription.
func (s *Store) GetSubscriptionView(ctx context.Context, feedID int64) (SubscriptionView, error) {
	v, err := scanSubscriptionView(s.Read.QueryRowContext(ctx, `
		SELECT `+subscriptionViewColumns+`
		FROM subscriptions s
		JOIN feeds f ON f.id = s.feed_id
		WHERE s.feed_id = ?
	`, feedID))
	if err == sql.ErrNoRows {
		return SubscriptionView{}, fmt.Errorf("store: get subscription view for feed %d: %w", feedID, ErrNotFound)
	}
	if err != nil {
		return SubscriptionView{}, fmt.Errorf("store: get subscription view for feed %d: %w", feedID, err)
	}
	return v, nil
}

// ListSubscriptionViews returns every subscription joined with its feed's
// display/crawl metadata, in display order — the shape the subscription
// list API needs.
func (s *Store) ListSubscriptionViews(ctx context.Context) ([]SubscriptionView, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT `+subscriptionViewColumns+`
		FROM subscriptions s
		JOIN feeds f ON f.id = s.feed_id
		ORDER BY s.sort_order, s.feed_id
	`)
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

// UpdateSubscription applies patch to feedID's subscription. Returns
// ErrNotFound if there's no subscription for that feed.
func (s *Store) UpdateSubscription(ctx context.Context, feedID int64, patch SubscriptionPatch) error {
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
	args = append(args, feedID)

	res, err := s.Write.ExecContext(ctx,
		`UPDATE subscriptions SET `+strings.Join(sets, ", ")+` WHERE feed_id = ?`, args...)
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
