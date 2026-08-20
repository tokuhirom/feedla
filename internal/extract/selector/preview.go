package selector

import (
	"bytes"
	"fmt"
	"net/url"
	"time"

	"golang.org/x/net/html"
)

// PreviewItem is one item_selector match as returned to the preview UI
// (§8.2), before any individual article page is fetched: Date/Summary come
// only from the listing page itself.
type PreviewItem struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Date    string `json:"date,omitempty"` // RFC3339, empty when date_selector found nothing parseable
	Summary string `json:"summary,omitempty"`
	Seen    bool   `json:"seen"`
}

// PreviewResult is the response body for both the pre-subscribe
// POST /scrape_sources/preview and the post-subscribe
// POST /scrape_sources/{id}/preview, for kind "selector" (§8.2).
type PreviewResult struct {
	Items     []PreviewItem `json:"items"`
	Matched   int           `json:"matched"`
	Truncated bool          `json:"truncated"`
	Warnings  []string      `json:"warnings,omitempty"`
}

// Preview runs the same item/link/title/date/summary extraction pipeline as
// Extract, but performs no diffing against state and has no side effects
// (no article page fetches, no state read/write) -- the read-only check
// behind both preview endpoints. seen (nil for the pre-subscribe endpoint,
// where nothing has ever been imported) marks which candidate URLs a saved
// source's state already considers imported.
func Preview(rawURL string, pageHTML []byte, cfg Config, now time.Time, seen map[string]bool) (PreviewResult, error) {
	listURL, err := url.Parse(rawURL)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("selector: parse url %q: %w", rawURL, err)
	}
	doc, err := html.Parse(bytes.NewReader(pageHTML))
	if err != nil {
		return PreviewResult{}, fmt.Errorf("selector: parse html: %w", err)
	}

	cands, matched, truncated, warnings, err := extractCandidates(doc, cfg, listURL, now)
	if err != nil {
		return PreviewResult{}, err
	}

	items := make([]PreviewItem, len(cands))
	for i, c := range cands {
		pi := PreviewItem{URL: c.url, Title: c.title, Summary: c.summary, Seen: seen[c.url]}
		if c.date != nil {
			pi.Date = c.date.Format(time.RFC3339)
		}
		items[i] = pi
	}

	return PreviewResult{Items: items, Matched: matched, Truncated: truncated, Warnings: warnings}, nil
}
