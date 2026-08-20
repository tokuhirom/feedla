package crawler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/fulltext"
	"github.com/tokuhirom/feedla/internal/fulltext/boilerplate"
	"github.com/tokuhirom/feedla/internal/store"
)

// boilerplateSession carries one feed's boilerplate-removal state across
// the article pages a single crawl extracts (internal/fulltext/boilerplate).
// Both fulltext paths -- feed_fulltext (fulltext.go) and selector
// (scrape_selector.go) -- run their per-page extraction through it.
//
// The state is loaded lazily, on the first page actually extracted, and
// saved only if at least one page was: a crawl that finds no new entries
// fetches nothing, and must not touch the row -- the same rule the selector
// design puts on a crawl with no candidates. Unlike a scrape source's
// state, this one is written before the crawl's entries are, since it says
// nothing about which entries the crawl produced.
//
// A session is not safe for concurrent use, and two crawls of the same feed
// can overlap (CrawlFeed runs on manual refresh regardless of the
// scheduler's claim), in which case the later save wins and the other
// crawl's observations are lost. That costs some convergence and nothing
// else, so it is left alone rather than locked.
type boilerplateSession struct {
	crawler *Crawler
	feedID  int64
	state   *boilerplate.State
	dirty   bool
}

func (c *Crawler) newBoilerplateSession(feedID int64) *boilerplateSession {
	return &boilerplateSession{crawler: c, feedID: feedID}
}

// load fetches the stored state on first use. A missing or unreadable row
// yields an empty state -- this is a quality optimization, and re-learning
// costs a few crawls of unstripped extraction, nothing more.
func (s *boilerplateSession) load(ctx context.Context) *boilerplate.State {
	if s.state != nil {
		return s.state
	}
	raw, err := s.crawler.store.GetFeedBoilerplate(ctx, s.feedID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		slog.Warn("crawler: boilerplate: load state", "feed_id", s.feedID, "error", err)
	}
	s.state = boilerplate.ParseState(raw)
	return s.state
}

// minStrippedTextRatio is the fraction of the unstripped extraction's text
// that the stripped one must keep. Removing chrome routinely cuts a page's
// extracted text by half or more -- that is the whole point -- so this is
// not a check on how much went away, but a floor that catches the one case
// where the state itself has gone wrong: a feed whose pages are near-
// duplicates of each other teaches this mechanism that the article body is
// chrome, and then most of the page disappears.
const minStrippedTextRatio = 0.25

// extract runs Readability over pageHTML with this feed's known boilerplate
// removed, and records what the page contained for future pages.
//
// If stripping produced nothing usable -- a parse or render failure, an
// extraction error, a body too short to be an article, or one that lost
// most of the original's text -- the unstripped page is extracted instead.
//
// Those checks bound how badly stripping can go wrong, but they cannot see
// a subtree that was genuinely part of the article: the result is still a
// long, well-formed body, just missing a paragraph. Keeping that from
// happening is boilerplate.Apply's job (it only removes link-dominated
// subtrees), not this fallback's.
func (s *boilerplateSession) extract(ctx context.Context, pageHTML []byte, pageURL *url.URL) (*fulltext.Article, error) {
	state := s.load(ctx)

	doc, err := html.Parse(bytes.NewReader(pageHTML))
	if err != nil {
		slog.Warn("crawler: boilerplate: parse page", "feed_id", s.feedID, "url", urlString(pageURL), "error", err)
		return fulltext.Extract(pageHTML, pageURL)
	}

	removed, sigs := boilerplate.Apply(doc, state)
	state.Observe(sigs)
	s.dirty = true
	if removed == 0 {
		return fulltext.Extract(pageHTML, pageURL)
	}

	original, origErr := fulltext.Extract(pageHTML, pageURL)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		slog.Warn("crawler: boilerplate: render stripped page, extracting the original",
			"feed_id", s.feedID, "url", urlString(pageURL), "error", err)
		return original, origErr
	}
	stripped, err := fulltext.Extract(buf.Bytes(), pageURL)
	switch {
	case err != nil:
		slog.Info("crawler: boilerplate: stripped page failed to extract, using the original",
			"feed_id", s.feedID, "url", urlString(pageURL), "removed", removed, "error", err)
	case stripped.TextLen < minFulltextChars:
		slog.Info("crawler: boilerplate: stripped page too short, using the original",
			"feed_id", s.feedID, "url", urlString(pageURL), "removed", removed,
			"text_len", stripped.TextLen, "min", minFulltextChars)
	case origErr == nil && float64(stripped.TextLen) < float64(original.TextLen)*minStrippedTextRatio:
		slog.Warn("crawler: boilerplate: stripping removed most of the page, using the original",
			"feed_id", s.feedID, "url", urlString(pageURL), "removed", removed,
			"text_len", stripped.TextLen, "original_text_len", original.TextLen,
			"pages_observed", state.Pages())
	default:
		slog.Debug("crawler: boilerplate: stripped repeated subtrees",
			"feed_id", s.feedID, "url", urlString(pageURL),
			"removed", removed, "pages_observed", state.Pages())
		return stripped, nil
	}
	return original, origErr
}

// urlString renders pageURL for logging. Callers reach this package after a
// successful fetch of the same URL, so a nil is not expected -- but a log
// call is not worth a panic over.
func urlString(pageURL *url.URL) string {
	if pageURL == nil {
		return ""
	}
	return pageURL.String()
}

// save persists the state if any page went through extract. Failures are
// logged and swallowed: unlike a scrape source's state, this one does not
// affect which entries a crawl produces, so losing an update costs nothing
// but a slower convergence.
func (s *boilerplateSession) save(ctx context.Context, now time.Time) {
	if !s.dirty || s.state == nil {
		return
	}
	raw, err := s.state.Marshal()
	if err != nil {
		slog.Warn("crawler: boilerplate: marshal state", "feed_id", s.feedID, "error", err)
		return
	}
	if err := s.crawler.store.PutFeedBoilerplate(ctx, s.feedID, raw, now); err != nil {
		slog.Warn("crawler: boilerplate: save state", "feed_id", s.feedID, "error", err)
	}
}
