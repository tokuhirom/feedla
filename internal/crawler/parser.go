package crawler

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"

	"github.com/tokuhirom/feedla/internal/store"
)

const (
	// maxEntriesPerFeed guards against a single malicious/misbehaving feed
	// flooding the store with entries in one crawl.
	maxEntriesPerFeed = 1000
	// maxBodyBytes truncates an individual entry's sanitized body.
	maxBodyBytes = 512 << 10 // 512 KiB
)

var bodyPolicy = bluemonday.UGCPolicy()

// ParsedFeed is a feed reduced to what the store needs: display metadata
// plus normalized, sanitized entries ready for UpsertEntries.
type ParsedFeed struct {
	Title   string
	SiteURL string
	Entries []store.EntryInput
}

// ParseFeed parses a feed response body (as returned by Fetcher.Fetch) into
// a ParsedFeed. feedURL is used as the base for resolving relative links,
// and now as the fallback published time for entries that don't carry one.
func ParseFeed(feedURL string, body []byte, now time.Time) (*ParsedFeed, error) {
	gf, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("crawler: parse feed %s: %w", feedURL, err)
	}

	base, _ := url.Parse(feedURL)

	n := len(gf.Items)
	if n > maxEntriesPerFeed {
		n = maxEntriesPerFeed
	}
	entries := make([]store.EntryInput, 0, n)
	for _, item := range gf.Items[:n] {
		entries = append(entries, normalizeItem(item, base, now))
	}

	return &ParsedFeed{
		Title:   strings.TrimSpace(gf.Title),
		SiteURL: resolveURL(base, gf.Link),
		Entries: entries,
	}, nil
}

func normalizeItem(item *gofeed.Item, base *url.URL, now time.Time) store.EntryInput {
	link := resolveURL(base, item.Link)

	rawBody := item.Content
	if rawBody == "" {
		rawBody = item.Description
	}
	body := truncateUTF8(bodyPolicy.Sanitize(rawBody), maxBodyBytes)

	guid := item.GUID
	if guid == "" {
		guid = link
	}
	if guid == "" {
		guid = hashHex(item.Title + rawBody)
	}

	author := ""
	if item.Author != nil {
		author = item.Author.Name
	}

	published := now
	if item.PublishedParsed != nil && !item.PublishedParsed.After(now.Add(time.Hour)) {
		published = *item.PublishedParsed
	}
	updated := published
	if item.UpdatedParsed != nil {
		updated = *item.UpdatedParsed
	}

	return store.EntryInput{
		GUID:        guid,
		URL:         link,
		Title:       strings.TrimSpace(item.Title),
		Author:      author,
		Body:        body,
		BodyHash:    hashBytes(body),
		PublishedAt: published.Unix(),
		UpdatedAt:   updated.Unix(),
		// Only "genuinely absent" counts, not "present but implausible" (the
		// >now+1h case above) -- that's a bad date, not a missing one, and
		// UpsertEntries' backlog-flood guard is specifically for feeds that
		// never carry dates at all.
		DateMissing: item.PublishedParsed == nil,
	}
}

func resolveURL(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if base == nil {
		return u.String()
	}
	return base.ResolveReference(u).String()
}

func hashBytes(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func hashHex(s string) string {
	return fmt.Sprintf("%x", hashBytes(s))
}

// truncateUTF8 cuts s to at most maxBytes bytes without splitting a rune.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 {
		if r, size := utf8.DecodeLastRuneInString(b); r != utf8.RuneError || size > 1 {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}
