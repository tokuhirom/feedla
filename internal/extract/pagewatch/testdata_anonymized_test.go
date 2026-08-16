package pagewatch

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"unicode"

	"golang.org/x/net/html"
)

// TestTestdataFixturesAreAnonymized machine-checks the promise in
// docs/feedless-site-subscription-pagewatch.md §14.5: every *.html file
// under testdata/ must contain no real page text. It exists so that
// accidentally committing a saved real page is caught by CI instead of
// relying on a human noticing in review.
//
// A fixture passes if every text node either (a) matches one of the -keep
// regexps recorded in its tools/htmlskeleton header comment, or (b) has
// every hiragana/katakana/kanji/ASCII-letter/digit rune already collapsed
// to the tool's placeholder (あ/ア/亜/a/A/0) — symbols and whitespace are
// unrestricted since the tool passes them through unchanged. Comment nodes
// must be empty.
var headerKeepRe = regexp.MustCompile(`(?s)<!--.*?-keep:\s*(.*?)\s*-->`)

func TestTestdataFixturesAreAnonymized(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("testdata", "*.html"))
	if err != nil {
		t.Fatalf("glob testdata: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no testdata/*.html fixtures found")
	}
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			checkFixtureAnonymized(t, path)
		})
	}
}

func checkFixtureAnonymized(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	keep := parseKeepPatterns(t, data)

	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.CommentNode:
			// Comments directly under the document (before <html>) are the
			// tool's own generated header, not scrubbed page content — they
			// are meant to carry provenance text, not be empty.
			if n.Parent != nil && n.Parent.Type == html.DocumentNode {
				break
			}
			if n.Data != "" {
				t.Errorf("non-empty comment node: %q", n.Data)
			}
		case html.TextNode:
			checkTextAnonymized(t, n.Data, keep)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

// parseKeepPatterns extracts the "-keep: p1, p2" list from the file's
// generated header comment; a plain "(none)" or a missing header (e.g. a
// hand-derived fixture with an extra leading comment, see akr-v2..v5.html)
// yields no patterns.
func parseKeepPatterns(t *testing.T, data []byte) []*regexp.Regexp {
	t.Helper()
	m := headerKeepRe.FindSubmatch(data)
	if m == nil || string(m[1]) == "(none)" || string(m[1]) == "" {
		return nil
	}
	var patterns []*regexp.Regexp
	for _, part := range regexp.MustCompile(`,\s*`).Split(string(m[1]), -1) {
		re, err := regexp.Compile(part)
		if err != nil {
			t.Fatalf("header -keep pattern %q does not compile: %v", part, err)
		}
		patterns = append(patterns, re)
	}
	return patterns
}

func checkTextAnonymized(t *testing.T, text string, keep []*regexp.Regexp) {
	t.Helper()
	for _, re := range keep {
		if re.MatchString(text) {
			return
		}
	}
	for _, r := range text {
		if !anonymizedRune(r) {
			t.Errorf("text node contains a non-anonymized character %q in: %s", r, excerpt(text))
			return
		}
	}
}

// anonymizedRune reports whether r is safe: either one of the tool's
// placeholder characters, or a symbol/whitespace rune the tool never
// touches (§14.5 table: "記号・空白はそのまま").
func anonymizedRune(r rune) bool {
	switch r {
	case 'あ', 'ア', '亜', 'a', 'A', '0':
		return true
	}
	if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Han, r) {
		return false
	}
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return false
	}
	return true
}

func excerpt(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return fmt.Sprintf("%q...", string(r[:40]))
	}
	return fmt.Sprintf("%q", s)
}
