// Package inspect turns a fetched third-party listing page into an
// allow-listed, safe-to-display subset plus a structural element index, for
// Phase F2's "click to build a CSS selector" GUI (§10 of
// docs/feedless-site-subscription-selector.md). It knows nothing about HTTP
// or the database -- Sanitize is a pure function -- and it never imports
// internal/extract: reducing a page for safe display is a different concern
// from parsing it for entries.
package inspect

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Element is one surviving node from Sanitize's allow-list walk, in
// document order. It carries only what the (not-yet-built) click-to-
// selector algorithm needs to reconstruct CSS selectors -- tag, class
// list, original id attribute, and parent linkage -- never text content or
// other attribute values.
type Element struct {
	ID       int      `json:"id"`                // matches the element's data-feedla-id
	Tag      string   `json:"tag"`               // e.g. "article"; img is reported as "img" even though it's rendered as a placeholder <span>
	Classes  []string `json:"classes,omitempty"` // from the (filtered) class attribute
	HTMLID   string   `json:"html_id,omitempty"` // original id="" attribute, if any (for a #id selector)
	ParentID int      `json:"parent_id"`         // 0 = no surviving ancestor (top-level under <body>); IDs start at 1, so 0 is never ambiguous
}

// allowedTags is the display-safe subset of HTML kept by Sanitize (§10.4).
// Everything else is dropped as a whole subtree -- including its
// descendants, not just the wrapping tag. Known limitation: a page that
// wraps its entire listing in a tag not on this list (a custom element /
// web component) loses that content. This mirrors a tradeoff the design
// doc already accepts elsewhere ("SPAの一覧ページは取れない", §14) rather
// than one invented here; softening it (e.g. unwrapping unknown tags
// instead of dropping them) is a design-doc-level decision, not something
// to change inside this implementation.
var allowedTags = map[string]bool{
	"div": true, "span": true, "p": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true,
	"dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"article": true, "section": true, "header": true, "footer": true,
	"nav": true, "aside": true, "main": true,
	"figure": true, "figcaption": true, "time": true,
	"a": true, "strong": true, "em": true, "b": true, "i": true, "small": true,
	"br": true, "hr": true, "style": true,
}

// voidTags mirrors x/net/html's own voidElements list for the tags we
// actually allow through (br, hr): html.Render errors if a void element
// ends up with children, which a malformed source page could otherwise
// trigger (e.g. an unclosed "<hr>" that a lenient parser nests content
// under).
var voidTags = map[string]bool{"br": true, "hr": true}

// allowedAttrs is the attribute allow-list for every kept tag. href/src are
// deliberately excluded: a link's destination isn't needed to build a
// selector, and keeping it would just be a navigation vector inside the
// sandboxed iframe. data-feedla-id is not in this list -- Sanitize assigns
// it itself and never trusts one from the input.
var allowedAttrs = map[string]bool{
	"class": true, "id": true, "style": true, "alt": true,
}

var (
	styleImportPattern = regexp.MustCompile(`(?i)@import\s+[^;]*;?`)
	styleURLPattern    = regexp.MustCompile(`(?i)url\(\s*[^)]*\)`)
)

// stripStyleRefs removes both forms of external-reference syntax CSS
// allows (§10.4): url(...) and the url()-free "@import "x.css";" form. Used
// on both the style="" attribute and <style> element text content.
func stripStyleRefs(css string) string {
	css = styleImportPattern.ReplaceAllString(css, "")
	css = styleURLPattern.ReplaceAllString(css, "")
	return css
}

// Sanitize reduces input to the allow-listed subset safe to show inside a
// sandboxed iframe under a locked-down CSP (§10.3-10.4), appends the
// click-detector script, and returns a structural index of the surviving
// elements alongside the rendered HTML.
func Sanitize(input []byte) (out []byte, elements []Element) {
	doc, err := html.Parse(bytes.NewReader(input))
	if err != nil {
		// html.Parse recovers from malformed input the way browsers do and
		// essentially never returns an error for real HTML bytes; this
		// branch just keeps Sanitize's signature error-free for callers.
		doc = &html.Node{Type: html.DocumentNode}
	}

	w := &walker{}
	newBody := &html.Node{Type: html.ElementNode, Data: "body"}
	if body := findBody(doc); body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			w.appendSanitized(newBody, c, 0)
		}
	}
	newBody.AppendChild(pickerScriptNode())

	root := &html.Node{Type: html.DocumentNode}
	root.AppendChild(&html.Node{Type: html.DoctypeNode, Data: "html"})
	htmlNode := &html.Node{Type: html.ElementNode, Data: "html"}
	htmlNode.AppendChild(&html.Node{Type: html.ElementNode, Data: "head"})
	htmlNode.AppendChild(newBody)
	root.AppendChild(htmlNode)

	var buf bytes.Buffer
	if err := html.Render(&buf, root); err != nil {
		return nil, nil
	}
	return buf.Bytes(), w.elements
}

func findBody(doc *html.Node) *html.Node {
	var body *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if body != nil || n == nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return body
}

// walker holds Sanitize's mutable state across the recursive tree walk:
// the next data-feedla-id to assign and the index accumulated so far.
type walker struct {
	nextID   int
	elements []Element
}

// appendSanitized processes n and, for surviving elements, its
// descendants, appending whatever survives as a child of dst. parentID is
// the data-feedla-id of the nearest surviving ancestor (0 if none yet).
func (w *walker) appendSanitized(dst, n *html.Node, parentID int) {
	switch n.Type {
	case html.TextNode:
		dst.AppendChild(&html.Node{Type: html.TextNode, Data: n.Data})
	case html.ElementNode:
		if n.Data == "img" {
			w.appendImgPlaceholder(dst, n, parentID)
			return
		}
		if !allowedTags[n.Data] {
			return // drop the whole subtree, not just the wrapping tag
		}
		w.nextID++
		id := w.nextID
		el := &html.Node{Type: html.ElementNode, Data: n.Data}
		classes, htmlID := w.filterAttrs(el, n)
		el.Attr = append(el.Attr, html.Attribute{Key: "data-feedla-id", Val: strconv.Itoa(id)})
		w.elements = append(w.elements, Element{ID: id, Tag: n.Data, Classes: classes, HTMLID: htmlID, ParentID: parentID})

		switch {
		case n.Data == "style":
			// A <style> element's only children are text nodes; apply the
			// same url()/@import stripping used on the style attribute.
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					el.AppendChild(&html.Node{Type: html.TextNode, Data: stripStyleRefs(c.Data)})
				}
			}
		case voidTags[n.Data]:
			// br/hr can't have children in valid output; a malformed
			// source page could otherwise hand us some.
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				w.appendSanitized(el, c, id)
			}
		}
		dst.AppendChild(el)
	default:
		// Comments, doctypes, etc. inside the body are dropped silently.
	}
}

// appendImgPlaceholder replaces <img> with a <span> carrying its alt text
// (§10.4). Reproducing the original box size via a style attribute is
// explicitly optional in the design and skipped here: nothing needs it to
// click the placeholder and build a selector.
func (w *walker) appendImgPlaceholder(dst, n *html.Node, parentID int) {
	w.nextID++
	id := w.nextID
	el := &html.Node{Type: html.ElementNode, Data: "span"}
	classes, htmlID := w.filterAttrs(el, n)
	el.Attr = append(el.Attr, html.Attribute{Key: "data-feedla-id", Val: strconv.Itoa(id)})
	w.elements = append(w.elements, Element{ID: id, Tag: "img", Classes: classes, HTMLID: htmlID, ParentID: parentID})
	if alt := attrVal(n, "alt"); alt != "" {
		el.AppendChild(&html.Node{Type: html.TextNode, Data: alt})
	}
	dst.AppendChild(el)
}

// filterAttrs copies src's allow-listed attributes onto dst (stripping
// style's url()/@import references along the way) and separately reports
// the class list and id, which the caller needs for the Element index.
func (w *walker) filterAttrs(dst, src *html.Node) (classes []string, htmlID string) {
	for _, a := range src.Attr {
		if !allowedAttrs[a.Key] {
			continue
		}
		val := a.Val
		if a.Key == "style" {
			val = stripStyleRefs(val)
		}
		dst.Attr = append(dst.Attr, html.Attribute{Key: a.Key, Val: val})
		switch a.Key {
		case "class":
			classes = strings.Fields(val)
		case "id":
			htmlID = val
		}
	}
	return classes, htmlID
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func pickerScriptNode() *html.Node {
	script := &html.Node{Type: html.ElementNode, Data: "script"}
	script.AppendChild(&html.Node{Type: html.TextNode, Data: pickerScript})
	return script
}
