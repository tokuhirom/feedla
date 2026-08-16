// Package pagewatch implements extract.Extractor for "Phase F0" (方式A):
// watching a single HTML page and turning content diffs into feed entries.
// See docs/feedless-site-subscription-pagewatch.md for the full design.
package pagewatch

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/extract"
)

// Extractor implements extract.Extractor for kind "pagewatch".
type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func (e *Extractor) Extract(ctx context.Context, in extract.Input) (*extract.Result, error) {
	cfg, err := ParseConfig(in.Config)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(in.URL)
	if err != nil {
		return nil, fmt.Errorf("pagewatch: parse url %q: %w", in.URL, err)
	}
	patterns, err := cfg.compiledIgnorePatterns()
	if err != nil {
		return nil, fmt.Errorf("pagewatch: compile ignore_patterns: %w", err)
	}

	doc, err := html.Parse(bytes.NewReader(in.HTML))
	if err != nil {
		return nil, fmt.Errorf("pagewatch: parse html: %w", err)
	}
	pageTitle := extractPageTitle(doc)
	body := findBody(doc)
	if body == nil {
		body = doc
	}

	removeNoise(body, 0)
	anchors := captureHeadingAnchors(body)
	filterAttrs(body, base)

	allBlocks := splitBlocks(body, anchors)
	blockCountTruncated := false
	if len(allBlocks) > MaxBlocks {
		allBlocks = allBlocks[:MaxBlocks]
		blockCountTruncated = true
	}
	if len(allBlocks) == 0 {
		return nil, fmt.Errorf("pagewatch: no content blocks found in %s (page structure changed, or the page is blocking automated access)", in.URL)
	}

	comparable := maskAndFilter(allBlocks, patterns)
	newConfigHash := cfg.configHash()
	newHash := contentHashBlocks(comparable)

	buildState := func() extract.Result {
		return extract.Result{
			Feed: emptyFeed(pageTitle, in.URL),
			State: State{
				Version:      CurrentStateVersion,
				RulesVersion: CurrentRulesVersion,
				ConfigHash:   newConfigHash,
				ContentHash:  newHash,
				Truncated:    blockCountTruncated,
				Blocks:       blocksToStateBlocks(comparable),
			}.marshal(),
		}
	}

	present := len(in.State) > 0
	prevState, valid := parseState(in.State)

	if !present {
		// True first run: nothing to diff against. Emit one "monitoring
		// started" entry so the subscription isn't silently empty, and save
		// a baseline state (§6.6).
		res := buildState()
		res.Feed = buildFeed(pageTitle, in.URL, in.Now, nil, nil, allBlocks, cfg, newHash, true)
		return &res, nil
	}

	if !valid || prevState.RulesVersion != CurrentRulesVersion {
		// Corrupt/unparseable state, or feedla's own removal rules changed
		// underneath it: resync silently, no entry (§6.6).
		res := buildState()
		return &res, nil
	}

	prevBlocks := prevState.Blocks
	if prevState.ConfigHash != newConfigHash {
		recomputed, ok := recomputeStateBlocks(prevState.Blocks, patterns)
		if !ok {
			// Saved display HTML was dropped for size, so we can't remask
			// it under the new patterns: resync, no entry (§6.6).
			res := buildState()
			return &res, nil
		}
		prevBlocks = recomputed
	}

	if contentHashStateBlocks(prevBlocks) == newHash {
		// No change at all: leave state untouched (§7.3).
		return &extract.Result{Feed: emptyFeed(pageTitle, in.URL), State: nil}, nil
	}

	added, removed := computeDiff(prevBlocks, comparable)

	if cfg.MinChangeChars > 0 && changeCharCount(added, removed) < cfg.MinChangeChars {
		res := buildState()
		return &res, nil
	}
	if cfg.watchMode() == WatchModeAdditions {
		removed = nil
	}
	if len(added) == 0 && len(removed) == 0 {
		res := buildState()
		return &res, nil
	}

	res := buildState()
	res.Feed = buildFeed(pageTitle, in.URL, in.Now, added, removed, allBlocks, cfg, newHash, false)
	return &res, nil
}

func emptyFeed(pageTitle, link string) *gofeed.Feed {
	return &gofeed.Feed{Title: displayTitle(pageTitle, link), Link: link}
}

func displayTitle(pageTitle, link string) string {
	if pageTitle != "" {
		return pageTitle
	}
	if u, err := url.Parse(link); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return link
}

// buildFeed renders the entry body (§5.2) and maps it into a gofeed.Feed
// with one Item (§5.1).
func buildFeed(pageTitle, link string, now time.Time, added, removed, allBlocks []Block, cfg Config, hash string, initial bool) *gofeed.Feed {
	title := displayTitle(pageTitle, link)

	var body strings.Builder
	if initial {
		body.WriteString("<p>監視を開始しました。</p>\n")
	} else {
		fmt.Fprintf(&body, "<p>%d ブロック追加 / %d ブロック削除</p>\n", len(added), len(removed))
		if len(added) > 0 {
			body.WriteString("<ins>\n")
			for _, b := range added {
				body.WriteString(b.HTML)
				body.WriteString("\n")
			}
			body.WriteString("</ins>\n")
		}
		if len(removed) > 0 {
			body.WriteString("<del>\n")
			for _, b := range removed {
				if b.HTML == "" {
					continue // display HTML was dropped for size (§6.3)
				}
				body.WriteString(b.HTML)
				body.WriteString("\n")
			}
			body.WriteString("</del>\n")
		}
	}
	if cfg.includeFullBody() {
		body.WriteString("<hr>\n<p>ページ全文（監視対象部分）</p>\n<div>\n")
		for _, b := range allBlocks {
			body.WriteString(b.HTML)
			body.WriteString("\n")
		}
		body.WriteString("</div>\n")
	}

	titleSuffix := "更新"
	if initial {
		titleSuffix = "監視開始"
	}
	itemTitle := fmt.Sprintf("%s — %s (%s)", title, titleSuffix, now.Format("01/02 15:04"))

	item := &gofeed.Item{
		Title:           itemTitle,
		Link:            link,
		GUID:            entryGUID(cfg, link, now, hash),
		Content:         body.String(),
		PublishedParsed: &now,
	}
	return &gofeed.Feed{Title: title, Link: link, Items: []*gofeed.Item{item}}
}

// entryGUID implements §5.3. In content mode the same GUID recurs whenever
// the page returns to a content state it has been in before (A→B→A), so a
// flapping page updates the one existing entry instead of piling up
// duplicates. Revision mode always mints a fresh GUID.
func entryGUID(cfg Config, link string, now time.Time, hash string) string {
	if cfg.guidMode() == GUIDModeRevision {
		return sha256Hex([]byte(link + "|" + now.Format(time.RFC3339Nano)))
	}
	return sha256Hex([]byte(hash))
}

func extractPageTitle(doc *html.Node) string {
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" {
			title = normalizeText(extractText(n))
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

func findBody(doc *html.Node) *html.Node {
	var body *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if body != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if body != nil {
				return
			}
		}
	}
	walk(doc)
	return body
}

var _ extract.Extractor = (*Extractor)(nil)
