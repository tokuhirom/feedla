package fulltext

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/extract"
)

// ExtractPublished looks for an article page's publish date, trying in
// order: the OGP article:published_time meta tag, JSON-LD datePublished
// (Article/NewsArticle/BlogPosting), then the first <time datetime> in
// document order (docs/feedless-site-subscription-selector.md §4.6, steps
// 2-4; step 1, the listing page's date_selector, is handled by
// internal/extract/selector itself since it needs no HTTP fetch).
func ExtractPublished(htmlBytes []byte, now time.Time) (time.Time, bool) {
	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return time.Time{}, false
	}

	if v := findMetaContent(doc, "article:published_time"); v != "" {
		if t, ok := extract.ParseFlexibleDate(v, now); ok {
			return t, true
		}
	}
	if v := findJSONLDDatePublished(doc); v != "" {
		if t, ok := extract.ParseFlexibleDate(v, now); ok {
			return t, true
		}
	}
	if v := findFirstTimeDatetime(doc); v != "" {
		if t, ok := extract.ParseFlexibleDate(v, now); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func findMetaContent(doc *html.Node, property string) string {
	var result string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if result != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "meta" {
			var prop, content string
			for _, a := range n.Attr {
				switch a.Key {
				case "property", "name":
					prop = a.Val
				case "content":
					content = a.Val
				}
			}
			if prop == property && content != "" {
				result = content
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if result != "" {
				return
			}
		}
	}
	walk(doc)
	return result
}

func findFirstTimeDatetime(doc *html.Node) string {
	var result string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if result != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "time" {
			if dt, ok := attrVal(n, "datetime"); ok && dt != "" {
				result = dt
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if result != "" {
				return
			}
		}
	}
	walk(doc)
	return result
}

func attrVal(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

var articleLikeJSONLDTypes = map[string]bool{
	"Article":     true,
	"NewsArticle": true,
	"BlogPosting": true,
}

// findJSONLDDatePublished scans every <script type="application/ld+json">
// on the page, in document order, for the first datePublished value
// belonging to an Article/NewsArticle/BlogPosting node (which may be
// nested inside an array or an @graph).
func findJSONLDDatePublished(doc *html.Node) string {
	var scripts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			isLD := false
			for _, a := range n.Attr {
				if a.Key == "type" && strings.EqualFold(a.Val, "application/ld+json") {
					isLD = true
				}
			}
			if isLD && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
				scripts = append(scripts, n.FirstChild.Data)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	for _, raw := range scripts {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			continue
		}
		if dp := scanJSONLD(v); dp != "" {
			return dp
		}
	}
	return ""
}

func scanJSONLD(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if articleLikeJSONLDTypes[jsonLDType(t["@type"])] {
			if dp, ok := t["datePublished"].(string); ok && dp != "" {
				return dp
			}
		}
		for _, val := range t {
			if dp := scanJSONLD(val); dp != "" {
				return dp
			}
		}
	case []any:
		for _, item := range t {
			if dp := scanJSONLD(item); dp != "" {
				return dp
			}
		}
	}
	return ""
}

func jsonLDType(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				return s
			}
		}
	}
	return ""
}
