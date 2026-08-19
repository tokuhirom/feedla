package fulltext

import (
	"net/url"
	"strings"
	"testing"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}

func TestExtract_ArticlePage(t *testing.T) {
	// Synthetic fixture (no third-party content): a typical article page
	// with nav/aside/footer noise around a real <article> body, long
	// enough that Readability's scoring picks it over the noise.
	html := `<!DOCTYPE html>
<html><head><title>Example Article</title></head>
<body>
<nav><a href="/">Home</a><a href="/about">About</a></nav>
<article>
<h1>Example Article</h1>
<p>` + strings.Repeat("This is the real article body, written as several sentences so Readability's content scoring favors it over the surrounding navigation and footer noise. ", 6) + `</p>
<p>A second paragraph continues the article with more substantive prose, again long enough to score well above the boilerplate around it.</p>
</article>
<aside><p>Related links go here.</p></aside>
<footer><p>Copyright 2026 Example Corp. All rights reserved.</p></footer>
</body></html>`

	art, err := Extract([]byte(html), mustParseURL(t, "https://example.com/articles/1"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(art.Content, "real article body") {
		t.Errorf("Content missing expected article text: %q", art.Content)
	}
	if strings.Contains(art.Content, "Copyright 2026") {
		t.Errorf("Content should not include footer noise: %q", art.Content)
	}
	if art.TextLen < 100 {
		t.Errorf("TextLen = %d, want a substantial article length", art.TextLen)
	}
}

func TestExtract_TooThin(t *testing.T) {
	// A page with essentially no prose content (e.g. a login wall or an
	// empty placeholder) -- callers use TextLen to reject this as a failed
	// extraction rather than storing near-empty entry bodies.
	html := `<!DOCTYPE html><html><body><p>Please log in.</p></body></html>`

	art, err := Extract([]byte(html), mustParseURL(t, "https://example.com/login"))
	if err == nil && art.TextLen > 50 {
		t.Errorf("expected a thin/empty extraction, got TextLen=%d content=%q", art.TextLen, art.Content)
	}
}
