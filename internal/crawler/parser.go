package crawler

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"regexp"
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

var bodyPolicy = newBodyPolicy()

// instagramPermalinkAttrPattern restricts data-instgrm-permalink (see
// newBodyPolicy) to genuine Instagram post/reel permalinks:
// "https://(www.)instagram.com/p|reel/<id>/", with an optional query string
// (real feeds append tracking params like "?utm_source=ig_embed"). id is
// deliberately conservative -- Instagram doesn't document a formal grammar
// for it, so this only allows the token characters actually seen in the
// wild, which in particular rules out "/" or "." escaping the path shape.
//
// This is a coarse, first-layer filter: the frontend (see
// web/src/utils/instagramEmbed.ts) re-validates the same shape itself
// before ever building an <iframe src> from it, so a bug here isn't the
// only thing standing between attacker-controlled feed content and the
// iframe's src.
var instagramPermalinkAttrPattern = regexp.MustCompile(
	`^https://(?:www\.)?instagram\.com/(?:p|reel)/[A-Za-z0-9_-]+/(?:\?[^"'<>\s]*)?$`,
)

// newBodyPolicy extends UGCPolicy with a narrow, regex-locked exception
// letting data-instgrm-permalink survive sanitization on a
// <blockquote class="instagram-media">, so the frontend can turn it into a
// sandboxed <iframe> for users who opt in (see
// docs/adr/0001-third-party-embed-in-feed-content.md). UGCPolicy already
// strips <script> and every other data-* attribute; this doesn't change
// that, and doesn't allow <iframe> here at all -- the iframe itself is only
// ever built client-side, from this one attribute.
func newBodyPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// UGCPolicy doesn't allow the "class" attribute at all (it deliberately
	// omits AllowStyling()); the frontend's selector needs it, so allow it
	// here but locked to exactly this one value rather than opening up
	// class styling in general.
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^instagram-media$`)).OnElements("blockquote")
	p.AllowAttrs("data-instgrm-permalink").Matching(instagramPermalinkAttrPattern).OnElements("blockquote")
	return p
}

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
	return parsedFeedFromGofeed(gf, feedURL, now), nil
}

// parsedFeedFromGofeed normalizes an already-parsed gofeed.Feed through the
// same sanitize/truncate/EntryInput pipeline as ParseFeed. Besides ParseFeed
// itself, this also backs the pagewatch integration in crawlOne: a
// gofeed.Feed synthesized by an internal/extract Extractor from a scraped
// HTML page goes through the exact same normalization as a real feed.
func parsedFeedFromGofeed(gf *gofeed.Feed, feedURL string, now time.Time) *ParsedFeed {
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
	}
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

// resolveURL resolves ref against base and returns the absolute URL, or ""
// if ref is empty, unparseable, or resolves to a scheme other than http(s).
// Feeds are attacker-controlled input: without this check, a malicious
// <link> (e.g. "javascript:...") would be stored verbatim and rendered as an
// <a href> by the frontend, relying solely on CSP script-src to neutralize
// it.
func resolveURL(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	resolved := u
	if base != nil {
		resolved = base.ResolveReference(u)
	}
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
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
