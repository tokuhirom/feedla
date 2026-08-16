package feed

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mmcdole/gofeed"
	xhtml "golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/crawler"
)

// Candidate is one feed found at or linked from a URL a user asked to
// subscribe to.
type Candidate struct {
	Title   string `json:"title"`
	FeedURL string `json:"feed_url"`
}

// DiscoverFeed resolves rawURL to one or more candidate feeds: if rawURL is
// itself a feed, it's the only candidate; if it's an HTML page, its
// <link rel="alternate"> feed tags become the candidates. Returns an error
// if fetching fails or no feed can be found either way.
func DiscoverFeed(ctx context.Context, fetcher *crawler.Fetcher, rawURL string) ([]Candidate, error) {
	result, err := fetcher.Fetch(ctx, rawURL, crawler.FetchOptions{})
	if err != nil {
		return nil, fmt.Errorf("feed: discover %s: %w", rawURL, err)
	}
	if result.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed: discover %s: unexpected status %d", rawURL, result.StatusCode)
	}

	feedURL := result.FinalURL
	if feedURL == "" {
		feedURL = rawURL
	}

	if parsed, err := gofeed.NewParser().Parse(bytes.NewReader(result.Body)); err == nil {
		title := strings.TrimSpace(parsed.Title)
		return []Candidate{{Title: title, FeedURL: feedURL}}, nil
	}

	candidates := extractAlternateLinks(result.Body, feedURL)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("feed: no feed found at or linked from %s", rawURL)
	}
	return candidates, nil
}

var feedLinkTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/json":      true,
	"application/feed+json": true,
}

// extractAlternateLinks scans an HTML document's <link rel="alternate">
// tags for feed types, resolving href against baseURL.
func extractAlternateLinks(body []byte, baseURL string) []Candidate {
	base, _ := url.Parse(baseURL)

	var candidates []Candidate
	z := xhtml.NewTokenizer(bytes.NewReader(body))
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			return candidates
		}
		if tt != xhtml.StartTagToken && tt != xhtml.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "link" || !hasAttr {
			continue
		}

		var rel, typ, href, title string
		for {
			key, val, more := z.TagAttr()
			switch string(key) {
			case "rel":
				rel = string(val)
			case "type":
				typ = string(val)
			case "href":
				href = string(val)
			case "title":
				title = string(val)
			}
			if !more {
				break
			}
		}

		if !strings.Contains(rel, "alternate") || !feedLinkTypes[strings.ToLower(strings.TrimSpace(typ))] || href == "" {
			continue
		}

		resolved := href
		if u, err := url.Parse(href); err == nil && base != nil {
			resolved = base.ResolveReference(u).String()
		}
		candidates = append(candidates, Candidate{
			Title:   xhtml.UnescapeString(title),
			FeedURL: xhtml.UnescapeString(resolved),
		})
	}
}
