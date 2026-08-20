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
// fetches nothing, and must not touch the row. This mirrors the selector
// design's rule that a crawl with no candidates writes no state at all.
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

// extract runs Readability over pageHTML with this feed's known boilerplate
// removed, and records what the page contained for future pages.
//
// If stripping produced nothing usable -- a parse or render failure, an
// extraction error, or a body too short to be the real article -- the
// unstripped page is extracted instead. That fallback is what makes the
// mechanism safe to run unconditionally: the worst case is the behavior
// feedla had before it existed.
func (s *boilerplateSession) extract(ctx context.Context, pageHTML []byte, pageURL *url.URL) (*fulltext.Article, error) {
	state := s.load(ctx)

	if doc, err := html.Parse(bytes.NewReader(pageHTML)); err == nil {
		removed, sigs := boilerplate.Apply(doc, state)
		state.Observe(sigs)
		s.dirty = true

		if removed > 0 {
			var buf bytes.Buffer
			if err := html.Render(&buf, doc); err == nil {
				article, err := fulltext.Extract(buf.Bytes(), pageURL)
				if err == nil && article.TextLen >= minFulltextChars {
					slog.Debug("crawler: boilerplate: stripped repeated subtrees",
						"feed_id", s.feedID, "url", pageURL.String(),
						"removed", removed, "pages_observed", state.Pages())
					return article, nil
				}
				slog.Info("crawler: boilerplate: stripped page unusable, extracting the original",
					"feed_id", s.feedID, "url", pageURL.String(), "removed", removed, "error", err)
			}
		}
	}

	return fulltext.Extract(pageHTML, pageURL)
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
