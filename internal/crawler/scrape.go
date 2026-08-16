package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tokuhirom/feedla/internal/extract"
)

// ScrapePrefix marks a feeds.feed_url as synthesized by an internal/extract
// method rather than fetched as a real feed — e.g.
// "pagewatch:https://example.com/diary/". A pseudo-scheme (rather than a
// join against scrape_sources) keeps ClaimDueFeeds/the Feed type unchanged,
// makes feed_url's existing UNIQUE constraint do double duty for scrape
// sources too, and fails safely (an XML parse error, not a silent scrape)
// if such a feed_url ever slipped into the real-feed code path by mistake.
// See docs/feedless-site-subscription-pagewatch.md §6.2.
const ScrapePrefix = "pagewatch:"

// PagewatchAccept is sent instead of the default feed Accept header when
// fetching a scrape source, so a server offering both an HTML page and an
// XML representation of the same URL returns the HTML one (§7.1).
const PagewatchAccept = "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8"

// extractPage looks up the pagewatch config/state for feedID, decodes fr's
// body to UTF-8, and runs it through the Extractor. The returned state is
// nil when nothing changed (extract.Result.State == nil) — callers must not
// persist a nil state as-is (§7.3 of the pagewatch design: nil means "leave
// the stored state alone", not "clear it").
func (c *Crawler) extractPage(ctx context.Context, feedID int64, targetURL string, fr *FetchResult, now time.Time) (*ParsedFeed, json.RawMessage, error) {
	src, err := c.store.GetScrapeSourceByFeedID(ctx, feedID)
	if err != nil {
		return nil, nil, fmt.Errorf("get scrape source: %w", err)
	}
	if src.Kind != string(extract.KindPageWatch) {
		return nil, nil, fmt.Errorf("unsupported scrape source kind %q", src.Kind)
	}

	body, err := DecodeUTF8(fr.Body, fr.ContentType)
	if err != nil {
		return nil, nil, fmt.Errorf("decode charset: %w", err)
	}

	result, err := c.pagewatch.Extract(ctx, extract.Input{
		URL:         targetURL,
		HTML:        body,
		ContentType: fr.ContentType,
		Now:         now,
		Config:      src.Config,
		State:       src.State,
	})
	if err != nil {
		return nil, nil, err
	}

	return parsedFeedFromGofeed(result.Feed, targetURL, now), result.State, nil
}
