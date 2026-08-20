// Package selector implements extract.Extractor for "Phase F1" (方式B1):
// pulling article links, titles, dates, and excerpts out of a listing page
// via user-supplied CSS selectors. See
// docs/feedless-site-subscription-selector.md for the full design.
//
// This package only knows how to parse the already-fetched listing page; it
// does not import internal/fulltext and performs no HTTP itself (§3.2).
// Fetching each new article and filling in its body is crawler's job.
package selector

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/extract"
)

// Extractor implements extract.Extractor for kind "selector".
type Extractor struct{}

func New() *Extractor { return &Extractor{} }

// candidate is one item_selector match, after link/title/date/summary
// extraction and URL normalization, before diffing against seen state.
type candidate struct {
	url     string
	title   string
	summary string
	date    *time.Time
}

func (e *Extractor) Extract(ctx context.Context, in extract.Input) (*extract.Result, error) {
	cfg, err := ParseConfig(in.Config)
	if err != nil {
		return nil, err
	}
	listURL, err := url.Parse(in.URL)
	if err != nil {
		return nil, fmt.Errorf("selector: parse url %q: %w", in.URL, err)
	}

	doc, err := html.Parse(bytes.NewReader(in.HTML))
	if err != nil {
		return nil, fmt.Errorf("selector: parse html: %w", err)
	}

	candidates, matched, truncated, _, err := extractCandidates(doc, cfg, listURL, in.Now)
	if err != nil {
		return nil, err
	}
	if matched == 0 {
		return nil, fmt.Errorf("selector: item_selector matched no elements in %s (page structure changed, the selector may be wrong, or the page is blocking automated access)", in.URL)
	}

	pageTitle := extractPageTitle(doc)
	feedTitle := displayTitle(pageTitle, in.URL)

	present := len(in.State) > 0
	prevState, valid := parseState(in.State)

	if present && !valid {
		// Corrupt state or unknown version: resync silently (§6.3). Seal
		// every current candidate into seen without creating any entries or
		// fetching any article pages.
		urls := make([]string, 0, len(candidates))
		for _, c := range candidates {
			urls = append(urls, c.url)
		}
		if len(urls) > MaxSeen {
			urls = urls[:MaxSeen]
		}
		resyncState := State{
			Version:    CurrentStateVersion,
			ConfigHash: cfg.ConfigHash(),
			Truncated:  truncated,
			Seen:       urls,
		}
		return &extract.Result{
			Feed:  &gofeed.Feed{Title: feedTitle, Link: in.URL},
			State: resyncState.marshal(),
		}, nil
	}

	seen := map[string]bool{}
	if valid {
		for _, u := range prevState.Seen {
			seen[u] = true
		}
	}

	var items []*gofeed.Item
	for _, c := range candidates {
		if seen[c.url] {
			continue
		}
		item := &gofeed.Item{
			Title:   c.title,
			Link:    c.url,
			GUID:    c.url,
			Content: c.summary,
		}
		if c.date != nil {
			item.PublishedParsed = c.date
		}
		items = append(items, item)
	}

	return &extract.Result{
		Feed:  &gofeed.Feed{Title: feedTitle, Link: in.URL, Items: items},
		State: nil, // crawler commits state via CommitState once it knows which candidates it actually imported (§6.5)
	}, nil
}

// extractCandidates runs the full per-item pipeline (§4.2-§4.7) and returns
// normalized, deduplicated candidates in document order, plus the raw
// item_selector match count (before truncation) and human-readable
// warnings for the preview UI (§8.2).
func extractCandidates(doc *html.Node, cfg Config, listURL *url.URL, now time.Time) (cands []candidate, matched int, truncated bool, warnings []string, err error) {
	itemSel, err := cascadia.Compile(cfg.ItemSelector)
	if err != nil {
		return nil, 0, false, nil, fmt.Errorf("selector: compile item_selector: %w", err)
	}
	linkSel, err := compileOptional(cfg.LinkSelector)
	if err != nil {
		return nil, 0, false, nil, err
	}
	titleSel, err := compileOptional(cfg.TitleSelector)
	if err != nil {
		return nil, 0, false, nil, err
	}
	dateSel, err := compileOptional(cfg.DateSelector)
	if err != nil {
		return nil, 0, false, nil, err
	}
	summarySel, err := compileOptional(cfg.SummarySelector)
	if err != nil {
		return nil, 0, false, nil, err
	}

	itemNodes := outermostMatches(itemSel.MatchAll(doc))
	matched = len(itemNodes)
	if matched == 0 {
		return nil, 0, false, nil, nil
	}
	if len(itemNodes) > MaxCandidates {
		itemNodes = itemNodes[:MaxCandidates]
		truncated = true
	}

	selfURL, _ := normalizeCandidateURL(listURL, listURL.String(), false)

	seenURLs := map[string]bool{}
	noLinkCount := 0
	dupCount := 0

	for _, item := range itemNodes {
		href, anchor, ok := extractLink(item, linkSel)
		if !ok {
			noLinkCount++
			continue
		}
		normURL, ok := normalizeCandidateURL(listURL, href, cfg.SameHostOnly())
		if !ok {
			continue
		}
		if normURL == selfURL {
			continue
		}
		if seenURLs[normURL] {
			dupCount++
			continue
		}
		seenURLs[normURL] = true

		cands = append(cands, candidate{
			url:     normURL,
			title:   extractTitle(item, titleSel, anchor),
			summary: extractSummary(item, summarySel),
			date:    extractDate(item, dateSel, now),
		})
	}

	if noLinkCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d 件の要素にリンクが無く、スキップされました", noLinkCount))
	}
	if dupCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d 件の要素が同じ URL を指していたため 1 件に畳まれました。link_selector の指定を検討してください", dupCount))
	}

	return cands, matched, truncated, warnings, nil
}

func compileOptional(sel string) (cascadia.Selector, error) {
	if sel == "" {
		return nil, nil
	}
	s, err := cascadia.Compile(sel)
	if err != nil {
		return nil, fmt.Errorf("selector: compile selector %q: %w", sel, err)
	}
	return s, nil
}

// outermostMatches drops any match whose ancestor is also a match (§4.2:
// "item_selector が入れ子でマッチした場合は外側だけを採る"), preserving the
// document order MatchAll returns.
func outermostMatches(nodes []*html.Node) []*html.Node {
	set := make(map[*html.Node]bool, len(nodes))
	for _, n := range nodes {
		set[n] = true
	}
	out := make([]*html.Node, 0, len(nodes))
	for _, n := range nodes {
		nested := false
		for p := n.Parent; p != nil; p = p.Parent {
			if set[p] {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, n)
		}
	}
	return out
}

func matchOrDescendant(item *html.Node, sel cascadia.Selector) *html.Node {
	if sel == nil {
		return nil
	}
	if sel.Match(item) {
		return item
	}
	if matches := sel.MatchAll(item); len(matches) > 0 {
		return matches[0]
	}
	return nil
}

func attrVal(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func firstAnchorWithHref(n *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			if href, ok := attrVal(n, "href"); ok && href != "" {
				found = n
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if found != nil {
				return
			}
		}
	}
	walk(n)
	return found
}

// longestLinkTextAnchor finds item's descendant <a href> with the longest
// normalized link text, ties broken by document order (§4.2).
func longestLinkTextAnchor(item *html.Node) *html.Node {
	var best *html.Node
	bestLen := -1
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if href, ok := attrVal(n, "href"); ok && href != "" {
				l := len([]rune(extract.NormalizeText(extract.TextContent(n))))
				if l > bestLen {
					bestLen = l
					best = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(item)
	return best
}

// extractLink implements §4.2's "リンクの取り出し方". It returns the raw
// (not yet normalized) href and the <a> element whose text doubles as the
// title fallback's step 2 (§4.7).
func extractLink(item *html.Node, linkSel cascadia.Selector) (href string, anchor *html.Node, ok bool) {
	if linkSel != nil {
		el := matchOrDescendant(item, linkSel)
		if el == nil {
			return "", nil, false
		}
		if el.Data == "a" {
			if h, hok := attrVal(el, "href"); hok && h != "" {
				return h, el, true
			}
			return "", nil, false
		}
		a := firstAnchorWithHref(el)
		if a == nil {
			return "", nil, false
		}
		h, _ := attrVal(a, "href")
		return h, a, true
	}
	if item.Type == html.ElementNode && item.Data == "a" {
		if h, hok := attrVal(item, "href"); hok && h != "" {
			return h, item, true
		}
		return "", nil, false
	}
	a := longestLinkTextAnchor(item)
	if a == nil {
		return "", nil, false
	}
	h, _ := attrVal(a, "href")
	return h, a, true
}

// normalizeCandidateURL implements §4.3 steps 1-5 (absolutize, scheme
// restriction, fragment removal, tracking-param removal, same-host check).
// Step 6 (self-URL exclusion) and step 7 (dedup) are the caller's job since
// they need cross-candidate state.
func normalizeCandidateURL(base *url.URL, raw string, sameHostOnly bool) (string, bool) {
	resolved := extract.ResolveURL(base, raw)
	u, err := url.Parse(resolved)
	if err != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	u.Fragment = ""
	u.RawFragment = ""
	if sameHostOnly && !sameHost(base, u) {
		return "", false
	}
	return u.String(), true
}

func sameHost(a, b *url.URL) bool {
	return stripWWW(a.Hostname()) == stripWWW(b.Hostname())
}

func stripWWW(host string) string {
	return strings.TrimPrefix(strings.ToLower(host), "www.")
}

// extractTitle implements the fulltext=true/false-shared prefix of §4.7's
// fallback chain (steps 1-3; steps 4-6 need the fetched article page and
// are crawler's job, applied only when this returns "").
func extractTitle(item *html.Node, titleSel cascadia.Selector, anchor *html.Node) string {
	if el := matchOrDescendant(item, titleSel); el != nil {
		if t := extract.NormalizeText(extract.TextContent(el)); t != "" {
			return t
		}
	}
	if anchor != nil {
		if t := extract.NormalizeText(extract.TextContent(anchor)); t != "" {
			return t
		}
	}
	return truncateRunes(extract.NormalizeText(extract.TextContent(item)), 120)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func extractDate(item *html.Node, dateSel cascadia.Selector, now time.Time) *time.Time {
	el := matchOrDescendant(item, dateSel)
	if el == nil {
		return nil
	}
	var raw string
	if el.Data == "time" {
		if dt, ok := attrVal(el, "datetime"); ok && dt != "" {
			raw = dt
		}
	}
	if raw == "" {
		raw = extract.NormalizeText(extract.TextContent(el))
	}
	if raw == "" {
		return nil
	}
	t, ok := extract.ParseFlexibleDate(raw, now)
	if !ok {
		return nil
	}
	return &t
}

func extractSummary(item *html.Node, summarySel cascadia.Selector) string {
	el := matchOrDescendant(item, summarySel)
	if el == nil {
		return ""
	}
	return extract.NormalizeText(extract.TextContent(el))
}

func extractPageTitle(doc *html.Node) string {
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" {
			title = extract.NormalizeText(extract.TextContent(n))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if title != "" {
				return
			}
		}
	}
	walk(doc)
	return title
}

func displayTitle(pageTitle, link string) string {
	if pageTitle != "" {
		return pageTitle
	}
	if u, err := url.Parse(link); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return link
}

var _ extract.Extractor = (*Extractor)(nil)
