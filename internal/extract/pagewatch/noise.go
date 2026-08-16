package pagewatch

import (
	"strings"

	"golang.org/x/net/html"
)

// removeTagsAlways are dropped regardless of where in the tree they appear.
var removeTagsAlways = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"svg": true, "canvas": true, "iframe": true, "object": true, "embed": true,
	"form": true, "input": true, "button": true, "select": true, "textarea": true, "label": true,
	"nav": true, "aside": true,
}

// removeTagsByDepth are dropped only within maxLandmarkDepth of <body>, so a
// site-wide header/footer is removed without catching a card's own <header>
// deep in the tree (§4.2a).
var removeTagsByDepth = map[string]bool{
	"header": true, "footer": true,
}

const maxLandmarkDepth = 3

var removeRoles = map[string]bool{
	"banner": true, "navigation": true, "contentinfo": true,
	"complementary": true, "search": true, "dialog": true, "alert": true,
}

// removeClassWords match whole words (split on -_ and whitespace) in id/class
// values, not substrings — "header-image" is dropped but "sub-headers-of-
// contents" is not (§4.2c).
var removeClassWords = map[string]bool{
	"nav": true, "navi": true, "navigation": true, "menu": true, "gnav": true, "gnavi": true,
	"breadcrumb": true, "breadcrumbs": true,
	"sidebar": true, "side": true, "aside": true, "footer": true, "header": true, "banner": true,
	"ad": true, "ads": true, "advertisement": true,
	"sns": true, "share": true, "social": true, "comment": true, "comments": true,
	"related": true, "recommend": true, "ranking": true,
	"pagination": true, "pager": true, "cookie": true, "cookiebar": true,
	"gotop": true, "pagetop": true, "skip": true,
	"lastmod": true, "lastmodified": true, "docid": true, "counter": true, "access": true,
}

// removeNoise mutates the tree rooted at n in place, unlinking nodes that
// match the hardcoded noise rules (§4.2). depth is n's distance from <body>
// (body's own direct children are depth 1); pass 0 for the initial call with
// n = <body>.
func removeNoise(n *html.Node, depth int) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		if c.Type == html.CommentNode {
			n.RemoveChild(c)
			continue
		}
		if c.Type == html.ElementNode && shouldRemove(c, depth+1) {
			n.RemoveChild(c)
			continue
		}
		removeNoise(c, depth+1)
	}
}

func shouldRemove(n *html.Node, depth int) bool {
	tag := n.Data
	if removeTagsAlways[tag] {
		return true
	}
	if removeTagsByDepth[tag] && depth <= maxLandmarkDepth {
		return true
	}
	if hasNoiseRole(attrVal(n, "role")) {
		return true
	}
	if hasNoiseWord(attrVal(n, "class")) || hasNoiseWord(attrVal(n, "id")) {
		return true
	}
	if isHidden(n) {
		return true
	}
	return false
}

func hasNoiseWord(v string) bool {
	if v == "" {
		return false
	}
	for _, word := range strings.FieldsFunc(v, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if removeClassWords[strings.ToLower(word)] {
			return true
		}
	}
	return false
}

func hasNoiseRole(v string) bool {
	if v == "" {
		return false
	}
	for _, tok := range strings.Fields(v) {
		if removeRoles[strings.ToLower(tok)] {
			return true
		}
	}
	return false
}

func isHidden(n *html.Node) bool {
	if hasAttr(n, "hidden") {
		return true
	}
	if strings.EqualFold(attrVal(n, "aria-hidden"), "true") {
		return true
	}
	style := strings.ReplaceAll(strings.ToLower(attrVal(n, "style")), " ", "")
	if strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") {
		return true
	}
	return false
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
