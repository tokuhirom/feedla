package crawler

import (
	"strings"
	"testing"
	"time"
)

// TestBodyPolicyKeepsValidInstagramPermalink exercises the full
// ParseFeed -> normalizeItem -> bodyPolicy.Sanitize pipeline, confirming
// the frontend actually gets data-instgrm-permalink to build an iframe
// from (see web/src/utils/instagramEmbed.ts): the attribute must survive
// bodyPolicy's otherwise-blanket data-* strip, but the embed <script> and
// the raw data-instgrm-captioned attribute must not.
func TestBodyPolicyKeepsValidInstagramPermalink(t *testing.T) {
	rss := `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Test Feed</title>
<link>https://example.com/</link>
<item>
  <title>Post</title>
  <link>https://example.com/posts/1</link>
  <guid>guid-1</guid>
  <description><![CDATA[<blockquote class="instagram-media" data-instgrm-captioned data-instgrm-permalink="https://www.instagram.com/p/Cabc123/?utm_source=ig_embed"><a>view</a></blockquote><script async src="//www.instagram.com/embed.js"></script>]]></description>
</item>
</channel></rss>`

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	parsed, err := ParseFeed("https://example.com/feed", []byte(rss), now)
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	body := parsed.Entries[0].Body
	if !strings.Contains(body, `data-instgrm-permalink="https://www.instagram.com/p/Cabc123/?utm_source=ig_embed"`) {
		t.Errorf("sanitized body must keep a valid permalink: %s", body)
	}
	if strings.Contains(body, "<script") {
		t.Errorf("sanitized body must not retain <script>: %s", body)
	}
	if strings.Contains(body, "data-instgrm-captioned") {
		t.Errorf("sanitized body must not retain unrelated data-* attributes: %s", body)
	}
	if !strings.Contains(body, `class="instagram-media"`) {
		t.Errorf("sanitized body must keep the instagram-media class (frontend selector depends on it): %s", body)
	}
}

func TestBodyPolicyDropsUnsafeInstagramPermalinks(t *testing.T) {
	cases := []struct {
		name      string
		permalink string
	}{
		{"wrong host", "https://www.instagram.com.evil.example/p/Cabc123/"},
		{"host suffix confusion", "https://evil.example/instagram.com/p/Cabc123/"},
		{"http scheme", "http://www.instagram.com/p/Cabc123/"},
		{"path traversal", "https://www.instagram.com/p/../admin/"},
		{"extra path segment", "https://www.instagram.com/p/Cabc123/extra/"},
		{"unknown post kind", "https://www.instagram.com/tv/Cabc123/"},
		{"missing trailing slash", "https://www.instagram.com/p/Cabc123"},
		{"not a permalink", "javascript:alert(1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `<blockquote class="instagram-media" data-instgrm-permalink="` + tc.permalink + `">fallback link</blockquote>`
			out := bodyPolicy.Sanitize(raw)
			if strings.Contains(out, "data-instgrm-permalink") {
				t.Errorf("permalink %q must be dropped: %s", tc.permalink, out)
			}
		})
	}
}
