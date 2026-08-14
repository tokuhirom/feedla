package store

import (
	"context"
	"fmt"
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
