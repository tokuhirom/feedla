package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

const (
	// maxFulltextFetchesPerCrawl caps how many entry pages a single crawl of
	// a fulltext-enabled feed will fetch, so a feed's first crawl (or a
	// sudden large backlog) can't turn one feed's turn in the crawl pool
	// into dozens of sequential extra fetches.
	maxFulltextFetchesPerCrawl = 20
	// minFulltextChars is the minimum extracted plain-text length to accept
	// as a real article body rather than falling back to the feed's own
	// summary (a login wall, a paywall teaser, or an extraction failure
	// typically yields far less than this).
	minFulltextChars = 200
)

// fulltextAccept is sent instead of the default feed Accept header when
// fetching an entry's own page for extraction, mirroring PagewatchAccept:
// feedla wants the HTML representation, not a possible feed/JSON alternate
// at the same URL.
const fulltextAccept = "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8"

// applyFulltext replaces the body of parsed.Entries that are new (not
// already in the store) with content extracted from each entry's own link,
// when feedID has fulltext extraction enabled (internal/store.FeedFulltext).
// This is unrelated to pagewatch/scrape sources -- feedID here is always a
// real, successfully-parsed feed; applyFulltext only enriches entry bodies
// already produced by ParseFeed.
//
// Extraction failures for individual entries are logged and left as the
// feed's own summary; they never fail the crawl.
func (c *Crawler) applyFulltext(ctx context.Context, feedID int64, parsed *ParsedFeed, now time.Time) {
	if len(parsed.Entries) == 0 {
		return
	}
	if _, err := c.store.GetFeedFulltext(ctx, feedID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("crawler: fulltext: check enabled", "feed_id", feedID, "error", err)
		}
		return
	}

	guids := make([]string, len(parsed.Entries))
	for i, e := range parsed.Entries {
		guids[i] = e.GUID
	}
	existing, err := c.store.ExistingEntryGUIDs(ctx, feedID, guids)
	if err != nil {
		slog.Warn("crawler: fulltext: check existing guids", "feed_id", feedID, "error", err)
		return
	}

	bp := c.newBoilerplateSession(feedID)
	defer bp.save(ctx, now)

	fetched := 0
	skipped := 0
	for i := range parsed.Entries {
		e := &parsed.Entries[i]
		if existing[e.GUID] {
			continue
		}
		if e.URL == "" {
			continue
		}
		if fetched >= maxFulltextFetchesPerCrawl {
			skipped++
			continue
		}
		fetched++

		body, err := c.extractEntryFulltext(ctx, e.URL, bp)
		if err != nil {
			slog.Warn("crawler: fulltext: extraction failed, keeping feed summary",
				"feed_id", feedID, "url", e.URL, "error", err)
			continue
		}
		e.Body = body
		e.BodyHash = hashBytes(body)
	}
	if skipped > 0 {
		slog.Warn("crawler: fulltext: per-crawl fetch cap reached, some new entries kept the feed summary",
			"feed_id", feedID, "cap", maxFulltextFetchesPerCrawl, "skipped", skipped)
	}
}

// extractEntryFulltext fetches pageURL and returns its sanitized, truncated
// main content, or an error if the fetch, extraction, or length threshold
// fails. bp carries the feed's boilerplate-removal state across the pages
// of this crawl.
func (c *Crawler) extractEntryFulltext(ctx context.Context, pageURL string, bp *boilerplateSession) (string, error) {
	fr, err := c.fetcher.Fetch(ctx, pageURL, FetchOptions{Accept: fulltextAccept})
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	if fr.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", fr.StatusCode)
	}

	html, err := DecodeUTF8(fr.Body, fr.ContentType)
	if err != nil {
		return "", fmt.Errorf("decode charset: %w", err)
	}

	u, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	article, err := bp.extract(ctx, html, u)
	if err != nil {
		return "", err
	}
	if article.TextLen < minFulltextChars {
		return "", fmt.Errorf("extracted content too short (%d chars)", article.TextLen)
	}

	return truncateUTF8(bodyPolicy.Sanitize(article.Content), maxBodyBytes), nil
}
