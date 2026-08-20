package pagewatch

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/extract"
)

// Block is one unit of diffable content (§4.4).
type Block struct {
	HTML   string // display: normalized HTML fragment
	Text   string // tags stripped, whitespace-normalized
	Key    string // comparison key: HTML with ignore_patterns masked out
	Anchor string // nearest preceding heading's id/name, if any
	Head   string // that heading's text
}

var blockTags = map[string]bool{
	"p": true, "li": true, "tr": true, "dt": true, "dd": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "pre": true, "figcaption": true,
	"td": true, "th": true, "section": true, "article": true, "div": true,
}

func isHeadingTag(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}

type headingState struct {
	anchor string
	head   string
}

// splitBlocks flattens the normalized DOM under root into blocks in
// document order. root should be <body> (or the whole document if there is
// no <body>) so that root-level bare text — text not wrapped in any block
// tag — can still surface as one block (§4.4's "body直下の裸テキスト").
//
// anchors maps a heading node to the id/name it carried before filterAttrs
// stripped it (headings aren't in the attribute allow-list, so by the time
// this runs the DOM itself no longer has it). Pass the map from
// captureHeadingAnchors, called before filterAttrs; nil is fine if callers
// don't care about Anchor/Head.
func splitBlocks(root *html.Node, anchors map[*html.Node]string) []Block {
	hs := &headingState{}
	return walkBlocks(root, hs, true, anchors)
}

func walkBlocks(n *html.Node, hs *headingState, isRoot bool, anchors map[*html.Node]string) []Block {
	var blocks []Block
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		blocks = append(blocks, walkBlocks(c, hs, false, anchors)...)
	}
	if len(blocks) > 0 {
		// A descendant already claimed this subtree's content as blocks, so
		// n itself (even if it's a block tag, e.g. a wrapping <div>) is not
		// also a block — it holds no content of its own.
		return blocks
	}
	if n.Type == html.ElementNode && blockTags[n.Data] {
		if blk, ok := makeBlock(n, hs); ok {
			if isHeadingTag(n.Data) {
				hs.anchor = anchors[n]
				hs.head = blk.Text
			}
			return []Block{blk}
		}
		return nil
	}
	if isRoot {
		if blk, ok := makeBlock(n, hs); ok {
			return []Block{blk}
		}
	}
	return nil
}

// captureHeadingAnchors records each heading's id/name (its own, or the
// first descendant's — e.g. <h2><a id="..."> — per §4.4) before filterAttrs
// would strip it. Call after removeNoise but before filterAttrs.
func captureHeadingAnchors(root *html.Node) map[*html.Node]string {
	out := map[*html.Node]string{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && isHeadingTag(n.Data) {
			if id := findAnchorID(n); id != "" {
				out[n] = id
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func findAnchorID(n *html.Node) string {
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode {
			if id := attrVal(n, "id"); id != "" {
				found = id
				return
			}
			if name := attrVal(n, "name"); name != "" {
				found = name
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if found != "" {
				return
			}
		}
	}
	walk(n)
	return found
}

func makeBlock(n *html.Node, hs *headingState) (Block, bool) {
	text := normalizeText(extractText(n))
	if text == "" && !containsMediaOrLink(n) {
		return Block{}, false
	}
	rendered := renderNode(n)
	return Block{
		HTML:   rendered,
		Text:   text,
		Key:    rendered,
		Anchor: hs.anchor,
		Head:   hs.head,
	}, true
}

func containsMediaOrLink(n *html.Node) bool {
	if n.Type == html.ElementNode && (n.Data == "img" || n.Data == "a") {
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if containsMediaOrLink(c) {
			return true
		}
	}
	return false
}

func extractText(n *html.Node) string {
	return extract.TextContent(n)
}

// normalizeText applies NFKC, folds &nbsp;/full-width space to a regular
// space, collapses whitespace runs to one space, and trims (§4.4).
func normalizeText(s string) string {
	return extract.NormalizeText(s)
}

func renderNode(n *html.Node) string {
	var b strings.Builder
	if err := html.Render(&b, n); err != nil {
		return ""
	}
	return b.String()
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string {
	return strings.TrimSpace(tagRe.ReplaceAllString(s, ""))
}

// applyIgnorePatterns replaces every pattern match in htmlFrag with "",
// operating on the rendered HTML string as specified in §4.5.
func applyIgnorePatterns(htmlFrag string, patterns []*regexp.Regexp) string {
	for _, re := range patterns {
		htmlFrag = re.ReplaceAllString(htmlFrag, "")
	}
	return htmlFrag
}

// maskAndFilter computes each block's comparison Key by masking its display
// HTML with patterns, dropping blocks whose masked text becomes empty
// (§4.5 steps 1-2) — those blocks don't participate in diffing at all.
func maskAndFilter(blocks []Block, patterns []*regexp.Regexp) []Block {
	out := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		masked := applyIgnorePatterns(b.HTML, patterns)
		if stripTags(masked) == "" {
			continue
		}
		b.Key = masked
		out = append(out, b)
	}
	return out
}

func changeCharCount(added, removed []Block) int {
	n := 0
	for _, b := range added {
		n += len([]rune(b.Text))
	}
	for _, b := range removed {
		n += len([]rune(b.Text))
	}
	return n
}
