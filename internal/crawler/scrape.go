package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// SelectorPrefix is ScrapePrefix's counterpart for kind "selector" (方式B1,
// Phase F1). See docs/feedless-site-subscription-selector.md §7.1.
const SelectorPrefix = "selector:"

// PagewatchAccept is sent instead of the default feed Accept header when
// fetching a scrape source's own listing/watched page, so a server offering
// both an HTML page and an XML representation of the same URL returns the
// HTML one (§7.1). Despite the name, both scrape kinds send it.
const PagewatchAccept = "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8"

// scrapePrefixes maps every known pseudo-scheme prefix to the extract.Kind
// it identifies. internal/store duplicates the pagewatch prefix's literal
// value as scrapePrefixLike/scrapePrefixLen (store must not import
// crawler); TestScrapePrefixesMatchStore checks the two stay in sync.
var scrapePrefixes = map[string]extract.Kind{
	ScrapePrefix:   extract.KindPageWatch,
	SelectorPrefix: extract.KindSelector,
}

// cutScrapePrefix strips whichever scrape prefix feedURL carries, returning
// the underlying target URL, the extract.Kind that prefix identifies, and
// whether feedURL was scrape-backed at all. A feedURL with no known prefix
// returns (feedURL, "", false).
func cutScrapePrefix(feedURL string) (target string, kind extract.Kind, ok bool) {
	for prefix, k := range scrapePrefixes {
		if rest, found := strings.CutPrefix(feedURL, prefix); found {
			return rest, k, true
		}
	}
	return feedURL, "", false
}

// prefixForKind returns the pseudo-scheme prefix for kind, for
// reconstructing feed_url after a permanent redirect (§7.1). Returns "" for
// a kind with no registered prefix, which should never happen for a kind
// crawlOne is actually dispatching on.
func prefixForKind(kind extract.Kind) string {
	for prefix, k := range scrapePrefixes {
		if k == kind {
			return prefix
		}
	}
	return ""
}

// PrefixForKind exports prefixForKind for internal/api, which needs to
// build a scrape source's feeds.feed_url from the kind a create request
// asked for.
func PrefixForKind(kind extract.Kind) string {
	return prefixForKind(kind)
}

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
