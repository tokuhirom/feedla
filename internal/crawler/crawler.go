// Package crawler implements feedla's fetch/parse/write pipeline: given a
// set of feeds, it fetches each (conditionally), parses new content, and
// writes entries to the store. Scheduling is fixed-interval for now;
// adaptive backoff and per-host politeness land in Phase 2.
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

// Crawler runs one crawl pass over a set of feeds.
type Crawler struct {
	store       *store.Store
	fetcher     *Fetcher
	concurrency int
}

// New builds a Crawler. concurrency <= 0 falls back to defaultConcurrency.
func New(st *store.Store, fetcher *Fetcher, concurrency int) *Crawler {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	return &Crawler{store: st, fetcher: fetcher, concurrency: concurrency}
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

// CrawlDue fetches every feed whose next_fetch_at has passed (capped at
// limit) and reports the outcome.
func (c *Crawler) CrawlDue(ctx context.Context, now time.Time, limit int) (*Summary, error) {
	feeds, err := c.store.ListDueFeeds(ctx, now, limit)
	if err != nil {
		return nil, fmt.Errorf("crawler: list due feeds: %w", err)
	}
	return c.crawlFeeds(ctx, feeds, now), nil
}

// CrawlAll fetches every known feed regardless of next_fetch_at.
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

// crawlOne fetches, parses and writes a single feed, recording the outcome
// on the feeds row (fixed-interval scheduling: next_fetch_at is always
// now + fetch_interval_sec, win or lose).
func (c *Crawler) crawlOne(ctx context.Context, f store.Feed, now time.Time) FeedResult {
	res := FeedResult{FeedID: f.ID, FeedURL: f.FeedURL}
	nextFetchAt := now.Unix() + f.FetchIntervalSec

	fail := func(status int, err error) FeedResult {
		res.Err = err
		msg := err.Error()
		if updErr := c.store.UpdateFeedAfterFetch(ctx, f.ID, store.FeedFetchUpdate{
			NextFetchAt: nextFetchAt,
			LastStatus:  status,
			Success:     false,
			LastError:   &msg,
		}, now); updErr != nil {
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
		return fail(0, err)
	}

	if fr.NotModified {
		if err := c.store.UpdateFeedAfterFetch(ctx, f.ID, store.FeedFetchUpdate{
			ETag:         nonEmptyPtr(fr.ETag),
			LastModified: nonEmptyPtr(fr.LastModified),
			NextFetchAt:  nextFetchAt,
			LastStatus:   fr.StatusCode,
			Success:      true,
		}, now); err != nil {
			res.Err = err
		}
		return res
	}

	if fr.StatusCode != http.StatusOK {
		return fail(fr.StatusCode, fmt.Errorf("crawler: unexpected status %d", fr.StatusCode))
	}

	parsed, err := ParseFeed(f.FeedURL, fr.Body, now)
	if err != nil {
		return fail(fr.StatusCode, err)
	}

	newCount, err := c.store.UpsertEntries(ctx, f.ID, parsed.Entries, now)
	if err != nil {
		return fail(fr.StatusCode, err)
	}
	res.NewEntries = newCount

	if err := c.store.UpdateFeedAfterFetch(ctx, f.ID, store.FeedFetchUpdate{
		Title:        parsed.Title,
		SiteURL:      parsed.SiteURL,
		ETag:         nonEmptyPtr(fr.ETag),
		LastModified: nonEmptyPtr(fr.LastModified),
		BodyHash:     hashBytes(string(fr.Body)),
		NextFetchAt:  nextFetchAt,
		LastStatus:   fr.StatusCode,
		Success:      true,
	}, now); err != nil {
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
