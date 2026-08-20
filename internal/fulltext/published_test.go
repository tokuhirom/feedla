package fulltext

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func TestExtractPublishedOGPPreferred(t *testing.T) {
	h := `<html><head>
<meta property="article:published_time" content="2026-08-18T09:00:00Z">
<script type="application/ld+json">{"@type":"NewsArticle","datePublished":"2026-08-17T00:00:00Z"}</script>
</head><body><time datetime="2026-08-16">x</time></body></html>`
	got, ok := ExtractPublished([]byte(h), testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractPublishedJSONLDFallback(t *testing.T) {
	h := `<html><head>
<script type="application/ld+json">{"@type":"BlogPosting","datePublished":"2026-08-17T00:00:00Z"}</script>
</head><body><time datetime="2026-08-16">x</time></body></html>`
	got, ok := ExtractPublished([]byte(h), testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractPublishedJSONLDGraph(t *testing.T) {
	h := `<html><head>
<script type="application/ld+json">{"@graph":[{"@type":"WebSite"},{"@type":["Article"],"datePublished":"2026-08-15T00:00:00Z"}]}</script>
</head><body></body></html>`
	got, ok := ExtractPublished([]byte(h), testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractPublishedTimeTagFallback(t *testing.T) {
	h := `<html><body><p>no meta here</p><time datetime="2026-08-16T00:00:00Z">Aug 16</time></body></html>`
	got, ok := ExtractPublished([]byte(h), testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractPublishedNoneFound(t *testing.T) {
	h := `<html><body><p>nothing structured here</p></body></html>`
	_, ok := ExtractPublished([]byte(h), testNow)
	if ok {
		t.Error("expected ok=false")
	}
}

func TestExtractPublishedFutureRejected(t *testing.T) {
	h := `<html><head><meta property="article:published_time" content="2030-01-01T00:00:00Z"></head><body></body></html>`
	_, ok := ExtractPublished([]byte(h), testNow)
	if ok {
		t.Error("expected future date to be rejected")
	}
}
