package crawler

import (
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const rssWithScript = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Test Feed</title>
<link>https://example.com/</link>
<item>
  <title>Hello</title>
  <link>/posts/1</link>
  <guid>guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[<p>safe</p><script>alert(1)</script>]]></description>
</item>
<item>
  <title>No GUID</title>
  <link>https://example.com/posts/2</link>
  <description>plain body</description>
</item>
</channel></rss>`

func TestParseFeedSanitizesAndResolvesLinks(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	parsed, err := ParseFeed("https://example.com/feed", []byte(rssWithScript), now)
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if parsed.Title != "Test Feed" {
		t.Errorf("Title = %q, want %q", parsed.Title, "Test Feed")
	}
	if len(parsed.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(parsed.Entries))
	}

	first := parsed.Entries[0]
	if first.GUID != "guid-1" {
		t.Errorf("first.GUID = %q, want guid-1", first.GUID)
	}
	if first.DateMissing {
		t.Error("first.DateMissing = true, want false: item has a <pubDate>")
	}
	if first.URL != "https://example.com/posts/1" {
		t.Errorf("first.URL = %q, want resolved absolute URL", first.URL)
	}
	if strings.Contains(first.Body, "<script>") {
		t.Errorf("body was not sanitized: %q", first.Body)
	}
	if !strings.Contains(first.Body, "<p>safe</p>") {
		t.Errorf("body lost safe markup: %q", first.Body)
	}

	second := parsed.Entries[1]
	if second.GUID == "" {
		t.Error("second.GUID: fallback GUID must not be empty when <guid> is missing")
	}
	if second.GUID != second.URL {
		t.Errorf("second.GUID = %q, want fallback to URL %q", second.GUID, second.URL)
	}
	if !second.DateMissing {
		t.Error("second.DateMissing = false, want true: item has no <pubDate>")
	}
	if second.PublishedAt != now.Unix() {
		t.Errorf("second.PublishedAt = %d, want crawl time %d as the fallback", second.PublishedAt, now.Unix())
	}
}

const rssWithDangerousLinks = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Evil Feed</title>
<link>javascript:alert(document.domain)</link>
<item>
  <title>JS link</title>
  <link>javascript:alert(1)</link>
  <guid>guid-js</guid>
  <description>body</description>
</item>
<item>
  <title>data link</title>
  <link>data:text/html,&lt;script&gt;alert(1)&lt;/script&gt;</link>
  <description>body</description>
</item>
</channel></rss>`

func TestParseFeedDropsNonHTTPSchemeLinks(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	parsed, err := ParseFeed("https://example.com/feed", []byte(rssWithDangerousLinks), now)
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if parsed.SiteURL != "" {
		t.Errorf("SiteURL = %q, want empty: javascript: scheme must be dropped", parsed.SiteURL)
	}
	if len(parsed.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(parsed.Entries))
	}
	for _, e := range parsed.Entries {
		if e.URL != "" {
			t.Errorf("entry %q: URL = %q, want empty: non-http(s) scheme must be dropped", e.Title, e.URL)
		}
	}
	// The javascript: item carries an explicit <guid>, so it must not fall
	// back to the (now-empty) link.
	if parsed.Entries[0].GUID != "guid-js" {
		t.Errorf("first.GUID = %q, want guid-js", parsed.Entries[0].GUID)
	}
	// The data: item has no <guid>, so it must fall back to a content hash,
	// not the empty link (which would collide with every other linkless
	// entry).
	if parsed.Entries[1].GUID == "" {
		t.Error("second.GUID: fallback GUID must not be empty when both <guid> and link are absent")
	}
}

func TestResolveURLSchemes(t *testing.T) {
	base, err := url.Parse("https://example.com/feed")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{"relative path inherits http(s) base scheme", "/posts/1", "https://example.com/posts/1"},
		{"absolute https", "https://other.example/x", "https://other.example/x"},
		{"absolute http", "http://other.example/x", "http://other.example/x"},
		{"javascript scheme dropped", "javascript:alert(1)", ""},
		{"data scheme dropped", "data:text/html,x", ""},
		{"mailto scheme dropped", "mailto:a@example.com", ""},
		{"empty ref", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveURL(base, c.ref); got != c.want {
				t.Errorf("resolveURL(base, %q) = %q, want %q", c.ref, got, c.want)
			}
		})
	}
}

func TestTruncateUTF8(t *testing.T) {
	s := "日本語のテスト文字列"
	for n := 0; n <= len(s)+2; n++ {
		got := truncateUTF8(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateUTF8(s, %d) = %q, not valid UTF-8", n, got)
		}
		if len(got) > n {
			t.Fatalf("truncateUTF8(s, %d) = %q, len %d exceeds limit", n, got, len(got))
		}
	}
}
