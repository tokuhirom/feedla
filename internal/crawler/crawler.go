// Package crawler implements feedla's fetch/parse/write pipeline: given a
// set of feeds, it fetches each (conditionally, politely per-host), parses
// new content, writes entries to the store, and adapts each feed's crawl
// interval to its observed update frequency and health.
package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tokuhirom/feedla/internal/extract"
	"github.com/tokuhirom/feedla/internal/extract/pagewatch"
	"github.com/tokuhirom/feedla/internal/store"
)

const defaultConcurrency = 32

// maxRecentInternalErrors caps the in-memory internalErrors ring buffer (see
// Crawler.recordInternalError) -- enough to survey a bad patch without
// growing unbounded during a long-running feedla-side outage.
const maxRecentInternalErrors = 50

// FetchRecorder receives one observation per crawled feed, for /metrics
// (see internal/metrics). Nil is a valid Crawler.metrics: recording becomes
// a no-op.
type FetchRecorder interface {
	ObserveFetch(status string, d time.Duration)
}

// InternalErrorEntry records one feedla-side crawl failure -- a store write
// that failed, not something the feed's publisher did wrong. Kept
// in-memory only (process-lifetime, not persisted): these are deliberately
// never written to the feeds row's error_count/last_error (see crawlOne),
// so this is the only place an operator can see them at all.
type InternalErrorEntry struct {
	FeedID  int64  `json:"feed_id"`
	FeedURL string `json:"feed_url"`
	Error   string `json:"error"`
	At      int64  `json:"at"`
}

// Crawler runs crawl passes over a set of feeds.
type Crawler struct {
	store       *store.Store
	fetcher     *Fetcher
	concurrency int
	minInterval time.Duration
	maxInterval time.Duration
	metrics     FetchRecorder
	// pagewatch is the only extract.Extractor Phase F0 has; a kind ->
	// Extractor registry can replace this if/when a second scrape method
	// (e.g. urlpattern) is added.
	pagewatch extract.Extractor

	internalErrMu  sync.Mutex
	internalErrors []InternalErrorEntry
}

// New builds a Crawler. concurrency <= 0 falls back to defaultConcurrency;
// minInterval/maxInterval <= 0 fall back to the docs/DESIGN.md defaults (10min/12h).
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
	return &Crawler{
		store: st, fetcher: fetcher, concurrency: concurrency, minInterval: minInterval, maxInterval: maxInterval,
		pagewatch: pagewatch.New(),
	}
}

// SetMetrics attaches a FetchRecorder that every subsequent crawl reports
// fetch outcomes to. Optional; pass nil to disable (the default).
func (c *Crawler) SetMetrics(m FetchRecorder) {
	c.metrics = m
}

// recordInternalError appends to the internalErrors ring buffer, trimming
// the oldest entry once maxRecentInternalErrors is exceeded.
func (c *Crawler) recordInternalError(feedID int64, feedURL string, err error, now time.Time) {
	c.internalErrMu.Lock()
	defer c.internalErrMu.Unlock()
	c.internalErrors = append(c.internalErrors, InternalErrorEntry{
		FeedID:  feedID,
		FeedURL: feedURL,
		Error:   err.Error(),
		At:      now.Unix(),
	})
	if over := len(c.internalErrors) - maxRecentInternalErrors; over > 0 {
		c.internalErrors = c.internalErrors[over:]
	}
}

// RecentInternalErrors returns a snapshot (oldest first, like the buffer's
// insertion order) of the most recent feedla-side crawl failures, for GET
// /api/v1/stats to surface -- see InternalErrorEntry.
func (c *Crawler) RecentInternalErrors() []InternalErrorEntry {
	c.internalErrMu.Lock()
	defer c.internalErrMu.Unlock()
	out := make([]InternalErrorEntry, len(c.internalErrors))
	copy(out, c.internalErrors)
	return out
}

// FeedResult is the outcome of crawling a single feed.
type FeedResult struct {
	FeedID     int64
	FeedURL    string
	NewEntries int
	Duration   time.Duration
	// Status is a short outcome label ("ok", "not_modified", "gone",
	// "error") for logging/metrics; it is set even when Err is nil.
	Status string
	Err    error
	// Internal is true when Err reflects a feedla-side failure (a store
	// write, typically) rather than something the feed's publisher did
	// wrong (bad HTTP status, unparseable body, network error). Internal
	// errors are never recorded on the feeds row's error_count/last_error
	// -- see crawlOne -- so a feedla-side hiccup doesn't get blamed on the
	// feed and doesn't push it toward the auto-unsubscribe threshold.
	Internal bool
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
	res := c.crawlAndReport(ctx, f, time.Now())
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
			results[i] = c.crawlAndReport(ctx, f, now)
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

// crawlAndReport wraps crawlOne with the cross-cutting observability bits
// from docs/DESIGN.md's "観測" section: a per-feed slog line (status, duration,
// new entries) and a /metrics observation. Every entry point that fetches
// a feed (the scheduler, `feedla crawl`, and the manual refresh endpoint)
// goes through this so none of them miss it.
func (c *Crawler) crawlAndReport(ctx context.Context, f store.Feed, now time.Time) FeedResult {
	start := time.Now()
	res := c.crawlOne(ctx, f, now)
	res.Duration = time.Since(start)

	if c.metrics != nil {
		c.metrics.ObserveFetch(res.Status, res.Duration)
	}
	if res.Internal {
		// A feedla-side failure (store write), not the feed publisher's
		// fault -- logged at Error (louder than the external-error Warn
		// below), tagged error_kind=internal so it doesn't get mistaken for
		// a flaky/broken feed while grepping logs, and kept in the
		// in-memory ring buffer GET /api/v1/stats surfaces (see
		// RecentInternalErrors) since it's deliberately never written to
		// the feed's own error_count/last_error.
		c.recordInternalError(res.FeedID, res.FeedURL, res.Err, now)
		slog.Error("crawler: fetch done", "feed_url", res.FeedURL, "status", res.Status,
			"duration_ms", res.Duration.Milliseconds(), "new_entries", res.NewEntries,
			"error_kind", "internal", "error", res.Err)
	} else if res.Err != nil {
		slog.Warn("crawler: fetch done", "feed_url", res.FeedURL, "status", res.Status,
			"duration_ms", res.Duration.Milliseconds(), "new_entries", res.NewEntries,
			"error_kind", "external", "error", res.Err)
	} else {
		slog.Info("crawler: fetch done", "feed_url", res.FeedURL, "status", res.Status,
			"duration_ms", res.Duration.Milliseconds(), "new_entries", res.NewEntries)
	}
	return res
}

// crawlOne fetches, parses and writes a single feed, then records the
// outcome on the feeds row with an adaptively recomputed
// fetch_interval_sec/next_fetch_at (see backoff.go).
func (c *Crawler) crawlOne(ctx context.Context, f store.Feed, now time.Time) FeedResult {
	res := FeedResult{FeedID: f.ID, FeedURL: f.FeedURL}
	currentInterval := time.Duration(f.FetchIntervalSec) * time.Second

	fail := func(status int, err error, retryAfter time.Duration, gone bool) FeedResult {
		res.Err = err
		if gone {
			res.Status = "gone"
		} else {
			res.Status = "error"
		}
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
			// Recording the external error itself failed (a feedla-side
			// store problem) -- logged separately as internal instead of
			// folded into res.Err, so what crawlAndReport reports for this
			// feed stays the error its publisher is actually responsible
			// for, not a "(and failed to record: ...)" mix of the two.
			slog.Error("crawler: failed to record fetch error", "feed_id", f.ID, "feed_url", f.FeedURL, "error", updErr)
		}
		return res
	}

	target, isScrape := strings.CutPrefix(f.FeedURL, ScrapePrefix)
	if !isScrape {
		target = f.FeedURL
	}

	etag, lastModified := "", ""
	if f.ETag != nil {
		etag = *f.ETag
	}
	if f.LastModified != nil {
		lastModified = *f.LastModified
	}

	opts := FetchOptions{ETag: etag, LastModified: lastModified}
	if isScrape {
		opts.Accept = PagewatchAccept
	}
	fr, err := c.fetcher.Fetch(ctx, target, opts)
	if err != nil {
		return fail(0, err, 0, false)
	}

	if fr.NotModified {
		res.Status = "not_modified"
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
			res.Internal = true
		}
		return res
	}

	if fr.StatusCode == http.StatusGone {
		return fail(fr.StatusCode, fmt.Errorf("crawler: 410 gone"), 0, true)
	}
	if fr.StatusCode != http.StatusOK {
		return fail(fr.StatusCode, fmt.Errorf("crawler: unexpected status %d", fr.StatusCode), fr.RetryAfter, false)
	}

	var parsed *ParsedFeed
	var scrapeState json.RawMessage
	if isScrape {
		parsed, scrapeState, err = c.extractPage(ctx, f.ID, target, fr, now)
	} else {
		parsed, err = ParseFeed(f.FeedURL, fr.Body, now)
		if err == nil {
			// Enriches parsed.Entries in place for feeds with fulltext
			// extraction enabled (internal/store.FeedFulltext) -- a no-op,
			// cheap point lookup for every other feed. Unrelated to the
			// isScrape/pagewatch branch above.
			c.applyFulltext(ctx, f.ID, parsed)
		}
	}
	if err != nil {
		// A 200 response that isn't actually a feed/page feedla can use (an
		// HTML login/error page instead of a feed, a page whose structure
		// left zero content blocks after noise removal, ...) parses fine as
		// HTTP but fails right here -- status/content-type turn "failed to
		// parse" from a dead end into something you can act on.
		wrapped := fmt.Errorf("%w (status=%d content-type=%q)", err, fr.StatusCode, fr.ContentType)
		return fail(fr.StatusCode, wrapped, 0, false)
	}

	newCount, err := c.store.UpsertEntries(ctx, f.ID, parsed.Entries, now)
	if err != nil {
		// A store write failure is feedla's problem, not the feed
		// publisher's -- unlike fail(), this deliberately leaves the feeds
		// row untouched (no error_count/last_error, no next_fetch_at
		// change) so a local/DB hiccup neither gets blamed on the feed nor
		// pushes it toward the auto-unsubscribe threshold. next_fetch_at
		// stays whatever ClaimDueFeeds already set, so the feed is simply
		// retried on the next scheduler tick.
		res.Err = fmt.Errorf("crawler: write entries: %w", err)
		res.Status = "error"
		res.Internal = true
		return res
	}
	res.NewEntries = newCount
	res.Status = "ok"

	// Persist state only when Extract actually returned one -- nil means
	// "nothing changed, leave the stored state as-is" (§7.3), and writing
	// unconditionally would turn every poll of an unchanged page into a
	// state-sized write.
	if isScrape && scrapeState != nil {
		if err := c.store.UpdateScrapeSourceState(ctx, f.ID, scrapeState, now); err != nil {
			res.Err = fmt.Errorf("crawler: save scrape state: %w", err)
			res.Status = "error"
			res.Internal = true
			return res
		}
	}

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
		newFeedURL := fr.FinalURL
		if isScrape {
			// Keep the pseudo-scheme: overwriting feed_url with the bare
			// target would make crawlOne treat this feed as a real feed on
			// every future crawl.
			newFeedURL = ScrapePrefix + newFeedURL
		}
		upd.NewFeedURL = &newFeedURL
	}
	if err := c.store.UpdateFeedAfterFetch(ctx, f.ID, upd, now); err != nil {
		res.Err = err
		res.Internal = true
	}
	return res
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
