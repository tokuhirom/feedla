// Package fulltext extracts a single article's main content from an
// already-fetched HTML page, for feeds whose entries carry only a short
// summary ("続きを読む" / "read more" style truncation) instead of the full
// article body.
//
// This is unrelated to internal/extract (the feedless/pagewatch machinery
// for sites that don't publish a feed at all): here a real feed and a real
// entry already exist, and the entry's own link is fetched a second time
// purely to enrich its body. Like internal/extract, this package has no
// dependency on internal/store or internal/crawler; the crawler package
// calls into this one, never the other way around.
package fulltext

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
)

// Article is one page's extracted main content.
type Article struct {
	Title string
	// Content is the extracted body as an HTML fragment. It is not
	// sanitized or size-limited -- callers must run it through the same
	// sanitize/truncate pipeline used for ordinary feed bodies before
	// storing or rendering it.
	Content string
	// textLen is the plain-text length of Content, used by callers to
	// reject extractions too short to be the real article body (e.g. a
	// page that redirected to a login wall).
	TextLen int
}

// Extract runs a Readability-style extraction against html (the raw bytes
// of a fetched page, already charset-decoded to UTF-8) and returns its main
// article content. pageURL is used to resolve relative links/images inside
// the extracted content.
func Extract(html []byte, pageURL *url.URL) (*Article, error) {
	article, err := readability.FromReader(bytes.NewReader(html), pageURL)
	if err != nil {
		return nil, fmt.Errorf("fulltext: extract %s: %w", pageURL, err)
	}
	if article.Node == nil {
		return nil, fmt.Errorf("fulltext: extract %s: no article content found", pageURL)
	}

	var htmlBuf, textBuf bytes.Buffer
	if err := article.RenderHTML(&htmlBuf); err != nil {
		return nil, fmt.Errorf("fulltext: render html %s: %w", pageURL, err)
	}
	if err := article.RenderText(&textBuf); err != nil {
		return nil, fmt.Errorf("fulltext: render text %s: %w", pageURL, err)
	}

	return &Article{
		Title:   strings.TrimSpace(article.Title()),
		Content: htmlBuf.String(),
		TextLen: len([]rune(strings.TrimSpace(textBuf.String()))),
	}, nil
}
