package extract

import (
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/text/unicode/norm"
)

// isTrackingParam reports whether key is a well-known click-tracking query
// parameter (utm_*, fbclid, gclid, ...) that should be stripped so a site
// varying it per-request doesn't itself change a page/article's identity.
func isTrackingParam(key string) bool {
	if strings.HasPrefix(key, "utm_") {
		return true
	}
	switch key {
	case "fbclid", "gclid", "_ga", "mc_cid", "mc_eid":
		return true
	}
	return false
}

// ResolveURL absolutizes raw against base and strips tracking query params.
// Invalid raw values are returned unchanged (best-effort; callers that need
// a valid absolute URL should url.Parse the result themselves). Shared by
// pagewatch and selector so both extraction methods treat link identity the
// same way.
func ResolveURL(base *url.URL, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	resolved := u
	if base != nil {
		resolved = base.ResolveReference(u)
	}
	if resolved.RawQuery != "" {
		q := resolved.Query()
		for key := range q {
			if isTrackingParam(key) {
				q.Del(key)
			}
		}
		resolved.RawQuery = q.Encode()
	}
	return resolved.String()
}

// NormalizeText applies NFKC, folds U+00A0/full-width space to a regular
// space, collapses whitespace runs to one space, and trims. Shared by
// pagewatch and selector so both compute the same display/comparison text
// from arbitrary page markup.
func NormalizeText(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	s = strings.ReplaceAll(s, "　", " ")
	s = norm.NFKC.String(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// TextContent concatenates all text node descendants of n, in document
// order, without any normalization.
func TextContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
