package selector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/extract"
)

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func parseDoc(t *testing.T, h string) *html.Node {
	t.Helper()
	doc, err := html.Parse(bytes.NewReader([]byte(h)))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	return doc
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func TestExtractCandidatesBasic(t *testing.T) {
	h := `<html><body>
<article><a href="/news/1">First post</a><time datetime="2026-08-18">Aug 18</time></article>
<article><a href="/news/2">Second post</a><time datetime="2026-08-19">Aug 19</time></article>
</body></html>`
	cfg := Config{ItemSelector: "article", DateSelector: "time"}
	cands, matched, truncated, warnings, err := extractCandidates(parseDoc(t, h), cfg, mustURL(t, "https://example.com/list"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if matched != 2 || truncated || len(warnings) != 0 {
		t.Fatalf("matched=%d truncated=%v warnings=%v", matched, truncated, warnings)
	}
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2", len(cands))
	}
	if cands[0].url != "https://example.com/news/1" || cands[0].title != "First post" {
		t.Errorf("cand0 = %+v", cands[0])
	}
	if cands[0].date == nil || !cands[0].date.Equal(time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("cand0 date = %v", cands[0].date)
	}
	if cands[1].url != "https://example.com/news/2" {
		t.Errorf("cand1 = %+v", cands[1])
	}
}

func TestExtractCandidatesItemIsAnchor(t *testing.T) {
	h := `<html><body><ul>
<li><a href="/a">Alpha</a></li>
<li><a href="/b">Beta</a></li>
</ul></body></html>`
	cfg := Config{ItemSelector: "li a"}
	cands, matched, _, _, err := extractCandidates(parseDoc(t, h), cfg, mustURL(t, "https://example.com/"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if matched != 2 || len(cands) != 2 {
		t.Fatalf("matched=%d cands=%d", matched, len(cands))
	}
	if cands[0].url != "https://example.com/a" || cands[0].title != "Alpha" {
		t.Errorf("cand0 = %+v", cands[0])
	}
}

func TestExtractCandidatesNoLinkWarns(t *testing.T) {
	h := `<html><body>
<article><a href="/a">Has link</a></article>
<article><span>No link here</span></article>
</body></html>`
	cfg := Config{ItemSelector: "article"}
	cands, matched, _, warnings, err := extractCandidates(parseDoc(t, h), cfg, mustURL(t, "https://example.com/"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if matched != 2 {
		t.Fatalf("matched = %d, want 2", matched)
	}
	if len(cands) != 1 {
		t.Fatalf("cands = %d, want 1", len(cands))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "リンクが無く") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestExtractCandidatesURLNormalizationDedup(t *testing.T) {
	h := `<html><body>
<article><a href="/news/1?utm_source=twitter">A</a></article>
<article><a href="/news/1#comments">A again</a></article>
</body></html>`
	cfg := Config{ItemSelector: "article"}
	cands, _, _, warnings, err := extractCandidates(parseDoc(t, h), cfg, mustURL(t, "https://example.com/"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("cands = %d, want 1 (dedup)", len(cands))
	}
	if cands[0].url != "https://example.com/news/1" {
		t.Errorf("cand0.url = %q", cands[0].url)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "畳まれました") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestExtractCandidatesSameHostOnly(t *testing.T) {
	h := `<html><body>
<article><a href="https://example.com/local">Local</a></article>
<article><a href="https://other.example/remote">Remote</a></article>
</body></html>`
	cfg := Config{ItemSelector: "article"}
	cands, _, _, _, err := extractCandidates(parseDoc(t, h), cfg, mustURL(t, "https://example.com/"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if len(cands) != 1 || cands[0].url != "https://example.com/local" {
		t.Fatalf("cands = %+v, want only the local link", cands)
	}

	allowOther := false
	cfg2 := Config{ItemSelector: "article", SameHostOnlyOpt: &allowOther}
	cands2, _, _, _, err := extractCandidates(parseDoc(t, h), cfg2, mustURL(t, "https://example.com/"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if len(cands2) != 2 {
		t.Fatalf("cands2 = %+v, want both links kept", cands2)
	}
}

func TestExtractCandidatesSelfURLExcluded(t *testing.T) {
	h := `<html><body>
<article><a href="/list">Back to top</a></article>
<article><a href="/news/1">Real article</a></article>
</body></html>`
	cfg := Config{ItemSelector: "article"}
	cands, _, _, _, err := extractCandidates(parseDoc(t, h), cfg, mustURL(t, "https://example.com/list"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if len(cands) != 1 || cands[0].url != "https://example.com/news/1" {
		t.Fatalf("cands = %+v, want only the real article", cands)
	}
}

func TestExtractCandidatesTruncation(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := 0; i < MaxCandidates+10; i++ {
		b.WriteString(`<article><a href="/n">x</a></article>`)
	}
	b.WriteString("</body></html>")
	cfg := Config{ItemSelector: "article"}
	_, matched, truncated, _, err := extractCandidates(parseDoc(t, b.String()), cfg, mustURL(t, "https://example.com/"), testNow)
	if err != nil {
		t.Fatalf("extractCandidates: %v", err)
	}
	if matched != MaxCandidates+10 {
		t.Errorf("matched = %d, want %d", matched, MaxCandidates+10)
	}
	if !truncated {
		t.Error("expected truncated = true")
	}
}

func extractInput(url, html string, config, state json.RawMessage) extract.Input {
	return extract.Input{URL: url, HTML: []byte(html), Now: testNow, Config: config, State: state}
}

func TestExtractNoMatchIsError(t *testing.T) {
	e := New()
	cfgJSON, _ := json.Marshal(Config{ItemSelector: ".does-not-exist"})
	_, err := e.Extract(context.Background(), extractInput("https://example.com/", "<html><body><p>nothing here</p></body></html>", cfgJSON, nil))
	if err == nil {
		t.Fatal("expected error when item_selector matches nothing")
	}
}

func TestExtractDiffsAgainstSeenState(t *testing.T) {
	e := New()
	h := `<html><body>
<article><a href="/news/1">First</a></article>
<article><a href="/news/2">Second</a></article>
</body></html>`
	cfgJSON, _ := json.Marshal(Config{ItemSelector: "article"})

	prevState := State{Version: CurrentStateVersion, Seen: []string{"https://example.com/news/1"}}
	prevStateJSON, _ := json.Marshal(prevState)

	res, err := e.Extract(context.Background(), extractInput("https://example.com/", h, cfgJSON, prevStateJSON))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Feed.Items) != 1 {
		t.Fatalf("got %d items, want 1 new candidate", len(res.Feed.Items))
	}
	if res.Feed.Items[0].Link != "https://example.com/news/2" {
		t.Errorf("item link = %q", res.Feed.Items[0].Link)
	}
	if res.State != nil {
		t.Errorf("Extract should return nil State (crawler commits state), got %s", res.State)
	}
}

func TestExtractResyncOnCorruptState(t *testing.T) {
	e := New()
	h := `<html><body><article><a href="/news/1">First</a></article></body></html>`
	cfgJSON, _ := json.Marshal(Config{ItemSelector: "article"})

	res, err := e.Extract(context.Background(), extractInput("https://example.com/", h, cfgJSON, []byte("{not json")))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Feed.Items) != 0 {
		t.Errorf("resync should produce no items, got %d", len(res.Feed.Items))
	}
	if res.State == nil {
		t.Fatal("resync should write state")
	}
	var st State
	if err := json.Unmarshal(res.State, &st); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if len(st.Seen) != 1 || st.Seen[0] != "https://example.com/news/1" {
		t.Errorf("resync state.Seen = %v", st.Seen)
	}
}
