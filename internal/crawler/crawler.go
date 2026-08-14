// Package crawler implements feedla's fetch/parse/write pipeline: given a
// set of feeds, it fetches each (conditionally, politely per-host), parses
// new content, writes entries to the store, and adapts each feed's crawl
// interval to its observed update frequency and health.
package crawler

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

const defaultConcurrency = 32

// Crawler runs crawl passes over a set of feeds.
type Crawler struct {
	store       *store.Store
	fetcher     *Fetcher
	concurrency int
	minInterval time.Duration
	maxInterval time.Duration
}

// New builds a Crawler. concurrency <= 0 falls back to defaultConcurrency;
// minInterval/maxInterval <= 0 fall back to the README defaults (10min/12h).
func New(st *store.Store, fetcher *Fetcher, concurrency int, minInterval, maxInterval time.Duration) *Crawler {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	if minInterval <= 0 {
		minInterval = defaultMinInterval
	}
	if maxInterval <= 0 {
		maxInterval = defaultMaxInterval
	}
	return &Crawler{store: st, fetcher: fetcher, concurrency: concurrency, minInterval: minInterval, maxInterval: maxInterval}
}

// FeedResult is the outcome of crawling a single feed.
type FeedResult struct {
	FeedID     int64
	FeedURL    string
	NewEntries int
	Err        error
}

// Summary aggregates a crawl pass over multiple feeds.
type Summary struct {
	Feeds      int
	NewEntries int
	Errors     int
	Results    []FeedResult
}

// CrawlDue atomically claims every feed whose next_fetch_at has passed
// (capped at limit; see Store.ClaimDueFeeds) and crawls them. Safe to call
// repeatedly from a ticking scheduler: claiming prevents a feed still being
// fetched from a previous call from being dispatched again.
func (c *Crawler) CrawlDue(ctx context.Context, now time.Time, limit int) (*Summary, error) {
	feeds, err := c.store.ClaimDueFeeds(ctx, now, limit)
	if err != nil {
		return nil, fmt.Errorf("crawler: claim due feeds: %w", err)
	}
	return c.crawlFeeds(ctx, feeds, now), nil
}

// CrawlFeed fetches, parses and writes a single feed on demand (the API's
// manual-refresh endpoint, and a subscribe request's "get me entries right
// away" step), independent of its next_fetch_at.
func (c *Crawler) CrawlFeed(ctx context.Context, feedID int64) (*FeedResult, error) {
	f, err := c.store.GetFeed(ctx, feedID)
	if err != nil {
		return nil, fmt.Errorf("crawler: crawl feed %d: %w", feedID, err)
	}
	res := c.crawlOne(ctx, f, time.Now())
	return &res, nil
}

// CrawlAll fetches every known feed regardless of next_fetch_at. Intended
// for one-shot manual/CLI use, not the scheduler loop.
func (c *Crawler) CrawlAll(ctx context.Context, now time.Time) (*Summary, error) {
	feeds, err := c.store.ListFeeds(ctx)
	if err != nil {
		return nil, fmt.Errorf("crawler: list feeds: %w", err)
	}
	return c.crawlFeeds(ctx, feeds, now), nil
}

func (c *Crawler) crawlFeeds(ctx context.Context, feeds []store.Feed, now time.Time) *Summary {
	results := make([]FeedResult, len(feeds))

	sem := make(chan struct{}, c.concurrency)
	var wg sync.WaitGroup
	for i, f := range feeds {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f store.Feed) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = c.crawlOne(ctx, f, now)
		}(i, f)
	}
	wg.Wait()

	summary := &Summary{Feeds: len(feeds), Results: results}
	for _, r := range results {
		summary.NewEntries += r.NewEntries
		if r.Err != nil {
			summary.Errors++
		}
	}
	return summary
}

// crawlOne fetches, parses and writes a single feed, then records the
// outcome on the feeds row with an adaptively recomputed
// fetch_interval_sec/next_fetch_at (see backoff.go).
func (c *Crawler) crawlOne(ctx context.Context, f store.Feed, now time.Time) FeedResult {
	res := FeedResult{FeedID: f.ID, FeedURL: f.FeedURL}
	currentInterval := time.Duration(f.FetchIntervalSec) * time.Second

	fail := func(status int, err error, retryAfter time.Duration, gone bool) FeedResult {
		res.Err = err
		msg := err.Error()
		interval := nextIntervalOnError(currentInterval, f.ErrorCount+1, retryAfter, c.minInterval)
		upd := store.FeedFetchUpdate{
			FetchIntervalSec: int64(interval / time.Second),
			NextFetchAt:      now.Add(interval).Unix(),
			LastStatus:       status,
			Success:          false,
			Gone:             gone,
			LastError:        &msg,
		}
		if updErr := c.store.UpdateFeedAfterFetch(ctx, f.ID, upd, now); updErr != nil {
			res.Err = fmt.Errorf("%w (and failed to record: %s)", err, updErr)
		}
		return res
	}

	etag, lastModified := "", ""
	if f.ETag != nil {
		etag = *f.ETag
	}
	if f.LastModified != nil {
		lastModified = *f.LastModified
	}

	fr, err := c.fetcher.Fetch(ctx, f.FeedURL, etag, lastModified)
	if err != nil {
		return fail(0, err, 0, false)
	}

	if fr.NotModified {
		interval := nextIntervalOnSuccess(currentInterval, false, c.minInterval, c.maxInterval)
		if err := c.store.UpdateFeedAfterFetch(ctx, f.ID, store.FeedFetchUpdate{
			ETag:             nonEmptyPtr(fr.ETag),
			LastModified:     nonEmptyPtr(fr.LastModified),
			FetchIntervalSec: int64(interval / time.Second),
			NextFetchAt:      now.Add(interval).Unix(),
			LastStatus:       fr.StatusCode,
			Success:          true,
		}, now); err != nil {
			res.Err = err
		}
		return res
	}

	if fr.StatusCode == http.StatusGone {
		return fail(fr.StatusCode, fmt.Errorf("crawler: 410 gone"), 0, true)
	}
	if fr.StatusCode != http.StatusOK {
		return fail(fr.StatusCode, fmt.Errorf("crawler: unexpected status %d", fr.StatusCode), fr.RetryAfter, false)
	}

	parsed, err := ParseFeed(f.FeedURL, fr.Body, now)
	if err != nil {
		return fail(fr.StatusCode, err, 0, false)
	}

	newCount, err := c.store.UpsertEntries(ctx, f.ID, parsed.Entries, now)
	if err != nil {
		return fail(fr.StatusCode, err, 0, false)
	}
	res.NewEntries = newCount

	interval := nextIntervalOnSuccess(currentInterval, newCount > 0, c.minInterval, c.maxInterval)
	upd := store.FeedFetchUpdate{
		Title:            parsed.Title,
		SiteURL:          parsed.SiteURL,
		ETag:             nonEmptyPtr(fr.ETag),
		LastModified:     nonEmptyPtr(fr.LastModified),
		BodyHash:         hashBytes(string(fr.Body)),
		FetchIntervalSec: int64(interval / time.Second),
		NextFetchAt:      now.Add(interval).Unix(),
		LastStatus:       fr.StatusCode,
		Success:          true,
	}
	if fr.PermanentRedirect && fr.FinalURL != "" {
		upd.NewFeedURL = &fr.FinalURL
	}
	if err := c.store.UpdateFeedAfterFetch(ctx, f.ID, upd, now); err != nil {
		res.Err = err
	}
	return res
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
