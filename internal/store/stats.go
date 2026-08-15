package store

import (
	"context"
	"fmt"
	"time"
)

// Stats aggregates the gauges docs/DESIGN.md's "観測"/`GET /api/v1/stats` sections
// call for: feed/entry counts plus the feeds currently erroring.
type Stats struct {
	FeedsTotal    int64              `json:"feeds_total"`
	FeedsErroring int64              `json:"feeds_erroring"`
	EntriesUnread int64              `json:"entries_unread"`
	QueueDepth    int64              `json:"queue_depth"`
	DBSizeBytes   int64              `json:"db_size_bytes"`
	ErroringFeeds []SubscriptionView `json:"erroring_feeds"`
}

// GetStats computes a fresh snapshot of Stats by querying the store; there
// is no cached/background-updated state to go stale.
func (s *Store) GetStats(ctx context.Context, now time.Time) (Stats, error) {
	var stats Stats

	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM feeds`).Scan(&stats.FeedsTotal); err != nil {
		return Stats{}, fmt.Errorf("store: stats: count feeds: %w", err)
	}
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM feeds WHERE error_count > 0`).Scan(&stats.FeedsErroring); err != nil {
		return Stats{}, fmt.Errorf("store: stats: count erroring feeds: %w", err)
	}
	if err := s.Read.QueryRowContext(ctx, `SELECT COALESCE(SUM(unread_count), 0) FROM subscriptions`).Scan(&stats.EntriesUnread); err != nil {
		return Stats{}, fmt.Errorf("store: stats: sum unread: %w", err)
	}
	if err := s.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM feeds WHERE next_fetch_at <= ? AND error_count < 20`, now.Unix(),
	).Scan(&stats.QueueDepth); err != nil {
		return Stats{}, fmt.Errorf("store: stats: count due feeds: %w", err)
	}

	var pageCount, pageSize int64
	if err := s.Read.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount); err != nil {
		return Stats{}, fmt.Errorf("store: stats: page_count: %w", err)
	}
	if err := s.Read.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return Stats{}, fmt.Errorf("store: stats: page_size: %w", err)
	}
	stats.DBSizeBytes = pageCount * pageSize

	subs, err := s.ListSubscriptionViews(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("store: stats: list subscriptions: %w", err)
	}
	for _, sub := range subs {
		if sub.ErrorCount > 0 {
			stats.ErroringFeeds = append(stats.ErroringFeeds, sub)
		}
	}

	return stats, nil
}
