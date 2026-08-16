package pagewatch

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// allowedAttrs is the per-tag attribute allow-list (§4.3). Every other
// attribute (class, id, style, data-*, on*, srcset, width, height, ...) is
// dropped: those are exactly the attributes most likely to churn between
// fetches without the visible content changing.
var allowedAttrs = map[string]map[string]bool{
	"a":    {"href": true},
	"img":  {"src": true, "alt": true},
	"time": {"datetime": true},
}

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

// filterAttrs mutates the tree in place: keep only allow-listed attributes,
// resolving a/img URLs against base and stripping tracking query params.
func filterAttrs(n *html.Node, base *url.URL) {
	if n.Type == html.ElementNode {
		allowed := allowedAttrs[n.Data]
		var kept []html.Attribute
		for _, a := range n.Attr {
			if !allowed[a.Key] {
				continue
			}
			val := a.Val
			if (n.Data == "a" && a.Key == "href") || (n.Data == "img" && a.Key == "src") {
				val = resolveURL(base, val)
			}
			kept = append(kept, html.Attribute{Key: a.Key, Val: val})
		}
		n.Attr = kept
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		filterAttrs(c, base)
	}
}

// resolveURL absolutizes raw against base and strips tracking query params,
// so a site switching between relative/absolute link notation — or a
// tracking param changing per request — doesn't itself produce a diff.
func resolveURL(base *url.URL, raw string) string {
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
