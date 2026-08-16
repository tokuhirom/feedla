package pagewatch

import (
	"bytes"
	"fmt"
	"net/url"

	xhtml "golang.org/x/net/html"
)

// PreviewBlock is one content block as pagewatch sees it, for the
// "what would this ignore_pattern hide?" UI (§8.1's preview endpoint and
// §9.4's "ignore this block" recovery flow).
type PreviewBlock struct {
	Text   string `json:"text"`
	Masked bool   `json:"masked"`
}

// Preview runs the same noise-removal/attribute-filter/block-split/mask
// pipeline as Extract, but performs no diffing and has no side effects (no
// state in, no state out) — the read-only check behind
// POST /scrape_sources/{id}/preview.
func Preview(rawURL string, pageHTML []byte, cfg Config) ([]PreviewBlock, error) {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("pagewatch: parse url %q: %w", rawURL, err)
	}
	patterns, err := cfg.compiledIgnorePatterns()
	if err != nil {
		return nil, fmt.Errorf("pagewatch: compile ignore_patterns: %w", err)
	}

	doc, err := xhtml.Parse(bytes.NewReader(pageHTML))
	if err != nil {
		return nil, fmt.Errorf("pagewatch: parse html: %w", err)
	}
	body := findBody(doc)
	if body == nil {
		body = doc
	}
	removeNoise(body, 0)
	filterAttrs(body, base)
	blocks := splitBlocks(body, nil)

	out := make([]PreviewBlock, len(blocks))
	for i, b := range blocks {
		masked := applyIgnorePatterns(b.HTML, patterns)
		out[i] = PreviewBlock{Text: b.Text, Masked: stripTags(masked) == ""}
	}
	return out, nil
}
