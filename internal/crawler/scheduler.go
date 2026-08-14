package crawler

import (
	"context"
	"log/slog"
	"time"
)

const (
	defaultTickInterval = 30 * time.Second
	defaultBatchLimit   = 200
	hostSemGCInterval   = 10 * time.Minute
	hostSemGCIdleAfter  = 10 * time.Minute
)

// Scheduler drives a Crawler on a fixed tick, claiming and crawling due
// feeds each time. It's the daemon-mode counterpart to the one-shot
// Crawler.CrawlAll/CrawlDue calls used by the `feedla crawl` CLI.
type Scheduler struct {
	crawler      *Crawler
	hostSem      *HostSemaphore
	tickInterval time.Duration
	batchLimit   int
}

// NewScheduler builds a Scheduler. hostSem may be nil (skips periodic GC of
// per-host bookkeeping); tickInterval/batchLimit <= 0 fall back to defaults.
func NewScheduler(cr *Crawler, hostSem *HostSemaphore, tickInterval time.Duration, batchLimit int) *Scheduler {
	if tickInterval <= 0 {
		tickInterval = defaultTickInterval
	}
	if batchLimit <= 0 {
		batchLimit = defaultBatchLimit
	}
	return &Scheduler{crawler: cr, hostSem: hostSem, tickInterval: tickInterval, batchLimit: batchLimit}
}

// Run blocks, crawling due feeds every tickInterval, until ctx is canceled.
// It always returns a non-nil error: ctx.Err() on a clean shutdown.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()

	gcTicker := time.NewTicker(hostSemGCInterval)
	defer gcTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.tick(ctx)
		case <-gcTicker.C:
			if s.hostSem != nil {
				s.hostSem.GC(hostSemGCIdleAfter)
			}
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	now := time.Now()
	summary, err := s.crawler.CrawlDue(ctx, now, s.batchLimit)
	if err != nil {
		slog.Error("crawler: tick failed", "error", err)
		return
	}
	if summary.Feeds == 0 {
		return
	}
	slog.Info("crawler: tick done",
		"feeds", summary.Feeds, "new_entries", summary.NewEntries, "errors", summary.Errors)
	for _, r := range summary.Results {
		if r.Err != nil {
			slog.Warn("crawler: feed failed", "feed_url", r.FeedURL, "error", r.Err)
		}
	}
}
