package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestAnonymizeRune(t *testing.T) {
	cases := map[rune]rune{
		'あ': 'あ', 'ぬ': 'あ', // hiragana
		'ア': 'ア', 'ヌ': 'ア', // katakana
		'亜': '亜', '漢': '亜', // kanji
		'a': 'a', 'z': 'a',
		'A': 'A', 'Z': 'A',
		'0': '0', '9': '0',
		' ': ' ', '。': '。', '-': '-', '\n': '\n',
	}
	for in, want := range cases {
		if got := anonymizeRune(in); got != want {
			t.Errorf("anonymizeRune(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnonymizeText_KeepPattern(t *testing.T) {
	keep := regexpList{regexp.MustCompile(`^Last Modified:`)}
	in := "Last Modified: 2026-08-11"
	if got := anonymizeText(in, keep); got != in {
		t.Errorf("kept text was modified: got %q", got)
	}

	notKept := "本文です。Last Modified: 2026-08-11"
	got := anonymizeText(notKept, keep)
	if got == notKept {
		t.Errorf("text not matching -keep anchor should still be transliterated, got unchanged %q", got)
	}
	if strings.ContainsAny(got, "本文") {
		t.Errorf("kanji leaked through: %q", got)
	}
}

func TestRun_StructurePreservedTextAnonymized(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	out := filepath.Join(dir, "out.html")
	src := `<html><body><div class="lastmod" id="x"><p>こんにちは World 123</p><!-- secret note --></div></body></html>`
	if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(in, out, "https://example.net/", nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	var sawDiv, sawComment bool
	var text string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			sawDiv = true
			if class := attrOf(n, "class"); class != "lastmod" {
				t.Errorf("class attribute must be preserved as-is, got %q", class)
			}
			if id := attrOf(n, "id"); id != "x" {
				t.Errorf("id attribute must be preserved as-is, got %q", id)
			}
		}
		if n.Type == html.CommentNode && n.Parent != nil && n.Parent.Type != html.DocumentNode {
			sawComment = true
			if n.Data != "" {
				t.Errorf("comment must be emptied, got %q", n.Data)
			}
		}
		if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data == "p" {
			text = n.Data
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if !sawDiv {
		t.Fatal("div element missing from output: structure was not preserved")
	}
	if !sawComment {
		t.Fatal("comment node missing from output: structure was not preserved")
	}
	if strings.ContainsAny(text, "こんにちは") {
		t.Errorf("hiragana leaked through untransliterated: %q", text)
	}
	if want := "あああああ Aaaaa 000"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
