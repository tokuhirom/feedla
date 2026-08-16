package pagewatch

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func prepareBody(t *testing.T, rawHTML, baseURL string) *html.Node {
	t.Helper()
	body, _ := prepareBodyWithAnchors(t, rawHTML, baseURL)
	return body
}

// prepareBodyWithAnchors runs the same pipeline order Extract uses
// (removeNoise -> captureHeadingAnchors -> filterAttrs), since
// captureHeadingAnchors must run before filterAttrs strips id/name.
func prepareBodyWithAnchors(t *testing.T, rawHTML, baseURL string) (*html.Node, map[*html.Node]string) {
	t.Helper()
	doc, err := html.Parse(bytes.NewReader([]byte(rawHTML)))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	body := findBody(doc)
	if body == nil {
		t.Fatal("no <body> found")
	}
	removeNoise(body, 0)
	anchors := captureHeadingAnchors(body)
	filterAttrs(body, base)
	return body, anchors
}

func blockTexts(blocks []Block) string {
	texts := make([]string, len(blocks))
	for i, b := range blocks {
		texts[i] = b.Text
	}
	return strings.Join(texts, "\n")
}

func TestRemoveNoise_LandmarksAndClassWords(t *testing.T) {
	raw := `<html><head><title>T</title></head><body>
<nav>メニュー項目です</nav>
<header>サイトヘッダーです</header>
<div><div><div>
  <header><h3>カード内ヘッダーです</h3></header>
  <p>カードの本文です。十分な長さのテキストがあります。</p>
</div></div></div>
<div class="ad-banner"><p>広告バナーの本文です。</p></div>
<div class="sub-headers"><p>サブヘッダーズは残るはずのテキストです。</p></div>
<footer>サイトフッターです</footer>
</body></html>`
	body := prepareBody(t, raw, "https://example.com/")
	texts := blockTexts(splitBlocks(body, nil))

	for _, want := range []string{"カード内ヘッダーです", "カードの本文です", "サブヘッダーズは残るはず"} {
		if !strings.Contains(texts, want) {
			t.Errorf("blocks missing expected text %q; got:\n%s", want, texts)
		}
	}
	for _, notWant := range []string{"メニュー項目です", "サイトヘッダーです", "広告バナーの本文です", "サイトフッターです"} {
		if strings.Contains(texts, notWant) {
			t.Errorf("blocks retained noise text %q; got:\n%s", notWant, texts)
		}
	}
}

func TestRemoveNoise_Hidden(t *testing.T) {
	raw := `<html><body>
<div hidden><p>非表示要素のテキストです。</p></div>
<div style="display: none;"><p>displayノーンのテキストです。</p></div>
<div aria-hidden="true"><p>ariaヒドゥンのテキストです。</p></div>
<div role="banner"><p>ロールバナーのテキストです。</p></div>
<p>通常の表示テキストがここにあります。</p>
</body></html>`
	body := prepareBody(t, raw, "https://example.com/")
	texts := blockTexts(splitBlocks(body, nil))

	for _, notWant := range []string{"非表示要素", "displayノーン", "ariaヒドゥン", "ロールバナー"} {
		if strings.Contains(texts, notWant) {
			t.Errorf("blocks retained hidden text %q; got:\n%s", notWant, texts)
		}
	}
	if !strings.Contains(texts, "通常の表示テキスト") {
		t.Errorf("blocks missing visible text; got:\n%s", texts)
	}
}

func TestHasNoiseWord_ExactMatchOnly(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"ad-banner", true},
		{"sub-headers", false},
		{"header", true},
		{"headers", false},
		{"a2026_07_30_5", false},
	}
	for _, c := range cases {
		if got := hasNoiseWord(c.v); got != c.want {
			t.Errorf("hasNoiseWord(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}
