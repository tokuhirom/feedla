package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/extract"
	"github.com/tokuhirom/feedla/internal/extract/selector"
	"github.com/tokuhirom/feedla/internal/fulltext"
)

// extractSelectorPage runs the selector (方式B1) pipeline for feedID: parse
// the already-fetched listing page into new candidates
// (internal/extract/selector.Extract is HTTP/DB-free), fetch up to
// Config.MaxItemsPerCrawl of them (respecting robots.txt), extract each
// article's body/title/date, and fold the outcome into the next
// scrape_sources.state via selector.CommitState. See
// docs/feedless-site-subscription-selector.md §7.2.
func (c *Crawler) extractSelectorPage(ctx context.Context, feedID int64, listingURL string, fr *FetchResult, now time.Time) (*ParsedFeed, json.RawMessage, error) {
	src, err := c.store.GetScrapeSourceByFeedID(ctx, feedID)
	if err != nil {
		return nil, nil, fmt.Errorf("get scrape source: %w", err)
	}
	if src.Kind != string(extract.KindSelector) {
		return nil, nil, fmt.Errorf("unsupported scrape source kind %q", src.Kind)
	}

	cfg, err := selector.ParseConfig(src.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("parse selector config: %w", err)
	}

	body, err := DecodeUTF8(fr.Body, fr.ContentType)
	if err != nil {
		return nil, nil, fmt.Errorf("decode charset: %w", err)
	}

	initial := len(src.State) == 0

	result, err := c.selector.Extract(ctx, extract.Input{
		URL:         listingURL,
		HTML:        body,
		ContentType: fr.ContentType,
		Now:         now,
		Config:      src.Config,
		State:       src.State,
	})
	if err != nil {
		return nil, nil, err
	}

	if result.State != nil {
		// Resync mode (corrupt/unknown-version state): selector.Extract
		// already sealed every candidate into the new state and returned no
		// items. No article fetches, no CommitState (§6.3).
		return parsedFeedFromGofeed(result.Feed, listingURL, now), result.State, nil
	}

	candidates := result.Feed.Items
	if len(candidates) == 0 {
		// No new candidates this crawl: nothing to fetch, and state stays
		// untouched (§6.5's "候補が0件のクロールでは state は書かない").
		return parsedFeedFromGofeed(result.Feed, listingURL, now), nil, nil
	}

	allCandidateURLs := make([]string, len(candidates))
	for i, item := range candidates {
		allCandidateURLs[i] = item.Link
	}

	toProcess := candidates
	if maxFetch := cfg.MaxItemsPerCrawlEffective(); len(toProcess) > maxFetch {
		toProcess = toProcess[:maxFetch]
	}

	prevPending := map[string]int{}
	if prevState, ok := selector.ParseState(src.State); ok {
		prevPending = prevState.Pending
	}

	imported := make([]string, 0, len(toProcess))
	var fetchFailed []string
	finalItems := make([]*gofeed.Item, 0, len(toProcess))

	for _, item := range toProcess {
		articleURL := item.Link

		if !cfg.FulltextEnabled() {
			ensurePublished(item, now)
			imported = append(imported, articleURL)
			finalItems = append(finalItems, item)
			continue
		}

		if !c.robots.Allowed(ctx, c.fetcher, c.fetcher.UserAgent(), articleURL) {
			// Disallowed: the URL is still a real, user-readable article (it
			// came from a page the user registered), so import it
			// title/link-only rather than dropping it, and never retry
			// (§4.5, §7.4).
			ensurePublished(item, now)
			imported = append(imported, articleURL)
			finalItems = append(finalItems, item)
			continue
		}

		afr, ferr := c.fetcher.Fetch(ctx, articleURL, FetchOptions{Accept: fulltextAccept})
		if ferr != nil || afr.StatusCode != http.StatusOK {
			if giveUp(prevPending[articleURL]) {
				ensurePublished(item, now)
				imported = append(imported, articleURL)
				finalItems = append(finalItems, item)
			} else {
				fetchFailed = append(fetchFailed, articleURL)
			}
			continue
		}

		articleBody, decErr := DecodeUTF8(afr.Body, afr.ContentType)
		if decErr != nil {
			// Not a network problem -- retrying won't change the encoding.
			// Treat like an extraction failure: import title/link-only, no
			// retry (§4.5's "本文抽出失敗" row).
			ensurePublished(item, now)
			imported = append(imported, articleURL)
			finalItems = append(finalItems, item)
			continue
		}

		applySelectorArticleContent(item, articleURL, articleBody, now)
		imported = append(imported, articleURL)
		finalItems = append(finalItems, item)
	}

	result.Feed.Items = finalItems
	parsed := parsedFeedFromGofeed(result.Feed, listingURL, now)

	// An approximation: len(candidates) counts only new-since-last-crawl
	// candidates, so this can under-report truncation when some of the
	// (already-seen) truncated matches would have been new. Harmless: state
	// isn't exposed by any API view (§6.6), this only shapes what a future
	// preview endpoint's own, independent extraction call would report.
	truncated := len(candidates) >= selector.MaxCandidates

	newState := selector.CommitState(src.State, selector.CommitInput{
		Candidates:  allCandidateURLs,
		Imported:    imported,
		FetchFailed: fetchFailed,
		Initial:     initial,
		Truncated:   truncated,
		ConfigHash:  cfg.ConfigHash(),
	})

	return parsed, newState, nil
}

// giveUp reports whether an article that has now failed prevFailures+1 times
// should be imported title-only rather than retried again (§4.5).
func giveUp(prevFailures int) bool {
	return prevFailures+1 >= selector.MaxArticleRetries
}

// ensurePublished guarantees item.PublishedParsed is non-nil before it
// reaches normalizeItem, so crawler.normalizeItem never sets DateMissing for
// a selector-backed entry -- required by §4.6.1, since UpsertEntries floods
// unrelated batches' backlog-suppression logic onto any DateMissing entry.
func ensurePublished(item *gofeed.Item, now time.Time) {
	if item.PublishedParsed == nil {
		t := now
		item.PublishedParsed = &t
	}
}

// applySelectorArticleContent fills item's body, title, and published date
// from a successfully fetched article page (§4.5, §4.6 steps 2-4, §4.7
// steps 4-6). item.Content/Title may already carry list-page-derived values
// (§4.2's summary_selector, §4.7 steps 1-3) that this only overwrites when a
// better source succeeds.
func applySelectorArticleContent(item *gofeed.Item, articleURL string, articleBody []byte, now time.Time) {
	u, _ := url.Parse(articleURL)
	article, extractErr := fulltext.Extract(articleBody, u)
	if extractErr == nil {
		if article.TextLen >= minFulltextChars {
			item.Content = article.Content
		}
		if item.Title == "" {
			item.Title = article.Title
		}
	}
	if item.Title == "" {
		item.Title = htmlPageTitle(articleBody)
	}
	if item.Title == "" {
		item.Title = urlPathTail(articleURL)
	}

	if item.PublishedParsed == nil {
		if t, ok := fulltext.ExtractPublished(articleBody, now); ok {
			item.PublishedParsed = &t
		}
	}
	ensurePublished(item, now)
}

// htmlPageTitle returns body's <title> text, normalized, or "" if there is
// none (§4.7 step 5).
func htmlPageTitle(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" {
			title = extract.NormalizeText(extract.TextContent(n))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if title != "" {
				return
			}
		}
	}
	walk(doc)
	return title
}

// urlPathTail returns the last non-empty path segment of rawURL, used as
// the final title fallback (§4.7 step 6) when a page has neither a
// Readability title nor a <title>.
func urlPathTail(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	path := strings.TrimSuffix(u.Path, "/")
	tail := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		tail = path[idx+1:]
	}
	if unescaped, err := url.PathUnescape(tail); err == nil {
		tail = unescaped
	}
	if tail == "" {
		return rawURL
	}
	return tail
}
