package crawler

import (
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
