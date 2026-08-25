package feed

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

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

	links := extractAlternateLinks(result.Body, feedURL)
	if len(links) == 0 {
		if candidates := guessFeedCandidates(ctx, fetcher, feedURL); len(candidates) > 0 {
			return candidates, nil
		}
		return nil, fmt.Errorf("feed: no feed found at or linked from %s", rawURL)
	}

	return resolveCandidateTitles(ctx, fetcher, links, extractPageTitle(result.Body)), nil
}

// feedURLSuffixes are path suffixes some sites' feeds live at without ever
// advertising them via a <link rel="alternate"> tag on the page. Tried in
// order against baseURL's path after HTML scanning finds nothing; the first
// one that actually parses as a feed wins.
var feedURLSuffixes = []string{".rss", "feed", "rss", "atom.xml", "feed.xml", "rss.xml", "index.xml"}

// guessFeedURLTimeout bounds the whole guessFeedCandidates attempt, since it
// may issue several requests to the same slow or unresponsive host.
const guessFeedURLTimeout = 15 * time.Second

// guessFeedCandidates tries appending feedURLSuffixes to baseURL's path,
// returning the first one that fetches successfully and parses as a feed.
func guessFeedCandidates(ctx context.Context, fetcher *crawler.Fetcher, baseURL string) []Candidate {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, guessFeedURLTimeout)
	defer cancel()

	for _, suffix := range feedURLSuffixes {
		guess := base.JoinPath(suffix)
		guess.RawQuery = ""
		guess.Fragment = ""
		guessURL := guess.String()

		result, err := fetcher.Fetch(ctx, guessURL, crawler.FetchOptions{})
		if err != nil || result.StatusCode != http.StatusOK {
			continue
		}
		parsed, err := gofeed.NewParser().Parse(bytes.NewReader(result.Body))
		if err != nil {
			continue
		}

		feedURL := result.FinalURL
		if feedURL == "" {
			feedURL = guessURL
		}
		return []Candidate{{Title: strings.TrimSpace(parsed.Title), FeedURL: feedURL}}
	}
	return nil
}

var feedLinkTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/json":      true,
	"application/feed+json": true,
}

// feedFormatLabels gives a short human-readable format name for each known
// feed MIME type, used to disambiguate fallback titles (see
// resolveCandidateTitles) when a page offers more than one feed format.
var feedFormatLabels = map[string]string{
	"application/rss+xml":   "RSS",
	"application/atom+xml":  "Atom",
	"application/json":      "JSON Feed",
	"application/feed+json": "JSON Feed",
}

// alternateLink is a <link rel="alternate"> feed tag found on an HTML page,
// before its title has been resolved to a Candidate.
type alternateLink struct {
	Title   string
	FeedURL string
	Type    string // normalized MIME type, e.g. "application/rss+xml"
}

// extractAlternateLinks scans an HTML document's <link rel="alternate">
// tags for feed types, resolving href against baseURL.
func extractAlternateLinks(body []byte, baseURL string) []alternateLink {
	base, _ := url.Parse(baseURL)

	var links []alternateLink
	z := xhtml.NewTokenizer(bytes.NewReader(body))
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			return links
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

		normType := strings.ToLower(strings.TrimSpace(typ))
		if !strings.Contains(rel, "alternate") || !feedLinkTypes[normType] || href == "" {
			continue
		}

		resolved := href
		if u, err := url.Parse(href); err == nil && base != nil {
			resolved = base.ResolveReference(u).String()
		}
		links = append(links, alternateLink{
			Title:   xhtml.UnescapeString(title),
			FeedURL: xhtml.UnescapeString(resolved),
			Type:    normType,
		})
	}
}

// extractPageTitle returns the text content of an HTML document's <title>
// element, or "" if it has none. Used as a fallback candidate title when a
// linked feed can't be fetched or parsed for its own title.
func extractPageTitle(body []byte) string {
	z := xhtml.NewTokenizer(bytes.NewReader(body))
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			return ""
		}
		if tt != xhtml.StartTagToken {
			continue
		}
		if name, _ := z.TagName(); string(name) != "title" {
			continue
		}
		if z.Next() != xhtml.TextToken {
			return ""
		}
		return strings.TrimSpace(xhtml.UnescapeString(string(z.Text())))
	}
}

// candidateTitleFetchTimeout bounds the whole batch of per-candidate feed
// fetches in resolveCandidateTitles, since a slow or unresponsive feed
// shouldn't stall lookup of the others indefinitely.
const candidateTitleFetchTimeout = 15 * time.Second

// resolveCandidateTitles turns each discovered <link> into a Candidate,
// preferring the linked feed's own title (fetched and parsed with gofeed)
// over the <link title="..."> attribute: many sites fill that attribute with
// a generic format label (e.g. "RSS 2.0") rather than the site or feed name.
//
// When a candidate feed can't be fetched or parsed, it falls back to the
// HTML page's own <title>, suffixed with the feed's format (RSS/Atom/JSON
// Feed) so that, e.g., an RSS and an Atom version of the same page don't end
// up with identical, indistinguishable titles.
func resolveCandidateTitles(ctx context.Context, fetcher *crawler.Fetcher, links []alternateLink, pageTitle string) []Candidate {
	ctx, cancel := context.WithTimeout(ctx, candidateTitleFetchTimeout)
	defer cancel()

	candidates := make([]Candidate, len(links))
	for i, link := range links {
		candidates[i] = Candidate{Title: fallbackCandidateTitle(link, pageTitle), FeedURL: link.FeedURL}

		result, err := fetcher.Fetch(ctx, link.FeedURL, crawler.FetchOptions{})
		if err != nil || result.StatusCode != http.StatusOK {
			continue
		}
		parsed, err := gofeed.NewParser().Parse(bytes.NewReader(result.Body))
		if err != nil {
			continue
		}
		if title := strings.TrimSpace(parsed.Title); title != "" {
			candidates[i].Title = title
		}
		if result.FinalURL != "" {
			candidates[i].FeedURL = result.FinalURL
		}
	}
	return candidates
}

// fallbackCandidateTitle is used until (or unless) resolveCandidateTitles
// manages to fetch the real feed title: the page's own <title>, suffixed
// with the feed format so same-page candidates in different formats don't
// collide. Falls back further to the <link>'s own title attribute if the
// page has none.
func fallbackCandidateTitle(link alternateLink, pageTitle string) string {
	label := feedFormatLabels[link.Type]
	switch {
	case pageTitle != "" && label != "":
		return pageTitle + " (" + label + ")"
	case pageTitle != "":
		return pageTitle
	default:
		return link.Title
	}
}
