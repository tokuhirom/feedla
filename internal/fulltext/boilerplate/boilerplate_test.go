package boilerplate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// pageWithChrome is a synthetic article page: site chrome that repeats
// verbatim across the feed's pages, plus one article-specific body. The
// <head> carries more text than minSignatureTextLen (an inline stylesheet,
// as the pages this package targets tend to have), so that the guard
// keeping <head> out of the candidate set is actually exercised.
func pageWithChrome(body string) string {
	return `<!DOCTYPE html><html><head><base href="https://example.com/"><title>Example List</title>
<style type="text/css">body { background: #e0e0e0; margin: 0; padding: 0; font-size: 10pt; }</style></head>
<body>
<div class="nav"><ul>
<li><a href="/products/">Products and services offered by the site owner</a>
<li><a href="/resources/">Resources, documentation and community links</a>
<li><a href="/about/">About this site and how to get in touch with us</a>
</ul></div>
<div class="article">` + body + `</div>
<div class="footer"><a href="/generator/">Powered by an example site generator</a> - <a href="/legal/">terms of use</a></div>
</body></html>`
}

// apply parses page, strips known boilerplate, records what it saw, and
// returns the stripped markup along with the removal count.
func apply(t *testing.T, s *State, page string) (string, int) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	removed, sigs := Apply(doc, s)
	s.Observe(sigs)
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String(), removed
}

func TestChromeIsStrippedOnceItHasRepeated(t *testing.T) {
	s := ParseState(nil)

	// The first two pages have nothing to compare against yet: a subtree
	// has to be seen twice before it can be called chrome.
	for i, body := range []string{
		"<p>The first article body, distinct from every other page.</p>",
		"<p>The second article body, also distinct from the others.</p>",
	} {
		out, removed := apply(t, s, pageWithChrome(body))
		if removed != 0 {
			t.Fatalf("page %d: removed %d subtrees, want 0", i+1, removed)
		}
		if !strings.Contains(out, "Products and services") {
			t.Fatalf("page %d: nav went missing before it repeated twice", i+1)
		}
	}

	out, removed := apply(t, s, pageWithChrome("<p>The third article body, the one that matters.</p>"))
	if removed == 0 {
		t.Fatal("third page: nothing removed, want the repeated chrome gone")
	}
	if strings.Contains(out, "Products and services") {
		t.Errorf("third page: nav survived:\n%s", out)
	}
	if strings.Contains(out, "Powered by an example site generator") {
		t.Errorf("third page: footer survived:\n%s", out)
	}
	if !strings.Contains(out, `<base href="https://example.com/"/>`) {
		t.Errorf("third page: <base> was stripped:\n%s", out)
	}
	if !strings.Contains(out, "The third article body") {
		t.Errorf("third page: article body was stripped:\n%s", out)
	}
}

func TestHeadIsNeverStripped(t *testing.T) {
	s := ParseState(nil)
	var out string
	for i := 0; i < 4; i++ {
		out, _ = apply(t, s, pageWithChrome(fmt.Sprintf("<p>Article body number %d, unique to this page.</p>", i)))
	}
	// <base href> decides how Readability resolves every relative link and
	// image in the extracted content, and it is identical on every page of
	// a site -- exactly the shape this package removes elsewhere.
	if !strings.Contains(out, `<base href="https://example.com/"/>`) {
		t.Errorf("<base> was stripped, relative URLs would resolve against the wrong origin:\n%s", out)
	}
	if !strings.Contains(out, "<title>Example List</title>") {
		t.Errorf("<title> was stripped, the extractor's title fallback would break:\n%s", out)
	}
}

func TestBlockSharedByAMinorityOfPagesSurvives(t *testing.T) {
	// A recurring intro paragraph -- a serialized post's preamble, a
	// standing license note -- is body text, not chrome. It appears on two
	// pages out of many, so the ratio rule has to keep it.
	const intro = `<div class="intro"><p>This post is part of a series about a topic that spans several articles.</p></div>`
	s := ParseState(nil)
	for i := 0; i < 8; i++ {
		apply(t, s, pageWithChrome(fmt.Sprintf("<p>Article body number %d, unique to this page.</p>", i)))
	}
	for i := 0; i < 2; i++ {
		apply(t, s, pageWithChrome(intro+fmt.Sprintf("<p>Serialized part %d.</p>", i)))
	}

	out, _ := apply(t, s, pageWithChrome(intro+"<p>Serialized part three.</p>"))
	if !strings.Contains(out, "part of a series") {
		t.Errorf("intro shared by 2 of 11 pages was stripped as chrome:\n%s", out)
	}
	if strings.Contains(out, "Products and services") {
		t.Errorf("nav on every page should still be stripped:\n%s", out)
	}
}

func TestRepeatedProseIsNotStripped(t *testing.T) {
	// A block that is part of the article but appears on every single page
	// -- a standing intro, a recurring license note. Repetition cannot tell
	// it apart from chrome, and the caller's length check cannot catch its
	// loss either (the article survives, just missing a paragraph), so the
	// link-density rule is the only thing protecting it.
	const prose = `<div class="notice"><p>All the material published here is provided as is, with no warranty of any kind, express or implied.</p></div>`
	s := ParseState(nil)
	var out string
	for i := 0; i < 12; i++ {
		out, _ = apply(t, s, pageWithChrome(prose+fmt.Sprintf("<p>Article body number %d, unique to this page.</p>", i)))
	}
	if !strings.Contains(out, "provided as is") {
		t.Errorf("prose repeated on every page was stripped out of the article:\n%s", out)
	}
	// The link-dominated chrome on the very same pages still goes.
	if strings.Contains(out, "Products and services") {
		t.Errorf("nav survived:\n%s", out)
	}
}

func TestDeeplyNestedSubtreesAreNotCandidates(t *testing.T) {
	// Bounding candidate depth is what keeps a page's unique interior from
	// flooding State and evicting the chrome before it is learned.
	s := ParseState(nil)
	var out string
	var sigCountPerPage int
	for i := 0; i < 5; i++ {
		// Every ancestor of the repeated link carries this page's own text,
		// so nothing above it repeats: the link's own subtree is the only
		// thing that could be removed, and it sits past the depth limit.
		deep := `<div><a href="/deep/">A repeated link buried far below the top of the document tree</a>` +
			fmt.Sprintf("<span>, next to text unique to page %d.</span></div>", i)
		for j := 0; j < maxCandidateDepth+2; j++ {
			deep = "<div>" + deep + "</div>"
		}
		page := pageWithChrome(deep + fmt.Sprintf("<p>Article body number %d, unique to this page.</p>", i))
		doc, err := html.Parse(strings.NewReader(page))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		_, sigs := Apply(doc, s)
		s.Observe(sigs)
		sigCountPerPage = len(sigs)
		var buf bytes.Buffer
		if err := html.Render(&buf, doc); err != nil {
			t.Fatalf("render: %v", err)
		}
		out = buf.String()
	}

	if !strings.Contains(out, "buried far below") {
		t.Errorf("a subtree past the depth limit was removed:\n%s", out)
	}
	if sigCountPerPage > maxSignatures/10 {
		t.Errorf("recorded %d signatures for one page; State would be flooded", sigCountPerPage)
	}
}

func TestShortRepeatedTextIsLeftAlone(t *testing.T) {
	// Below minSignatureTextLen: a byline, a section label, a "Read more"
	// link. These repeat across a site's articles without being chrome
	// worth cutting, and recording them would fill State with noise.
	const label = `<p class="kicker">Analysis</p>`
	s := ParseState(nil)
	var out string
	for i := 0; i < 5; i++ {
		out, _ = apply(t, s, pageWithChrome(label+fmt.Sprintf("<p>Article body number %d, unique to this page.</p>", i)))
	}
	if !strings.Contains(out, "Analysis") {
		t.Errorf("short repeated label was stripped:\n%s", out)
	}
}

func TestStateSurvivesRoundTrip(t *testing.T) {
	s := ParseState(nil)
	for i := 0; i < 3; i++ {
		apply(t, s, pageWithChrome(fmt.Sprintf("<p>Article body number %d, unique to this page.</p>", i)))
	}
	raw, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	restored := ParseState(raw)
	if restored.Pages() != s.Pages() {
		t.Fatalf("restored pages = %d, want %d", restored.Pages(), s.Pages())
	}
	out, removed := apply(t, restored, pageWithChrome("<p>A fourth article body, unique to this page.</p>"))
	if removed == 0 || strings.Contains(out, "Products and services") {
		t.Errorf("restored state did not strip the chrome it had already learned:\n%s", out)
	}
}

func TestParseStateToleratesUnusableInput(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"nil":            nil,
		"empty":          json.RawMessage(""),
		"malformed":      json.RawMessage("{not json"),
		"wrong version":  json.RawMessage(`{"v":999,"pages":50,"counts":{"deadbeefdeadbeef":[50,50]}}`),
		"negative count": json.RawMessage(`{"v":1,"pages":4,"counts":{"deadbeefdeadbeef":[-3,2]}}`),
	} {
		s := ParseState(raw)
		if s == nil || s.counts == nil {
			t.Fatalf("%s: ParseState returned an unusable State", name)
		}
		if s.isBoilerplate("deadbeefdeadbeef") {
			t.Errorf("%s: unusable state was trusted", name)
		}
	}
}

func TestObserveDecaysSoARedesignCanConverge(t *testing.T) {
	s := ParseState(nil)
	for i := 0; i < decayPages; i++ {
		s.Observe([]string{"old"})
	}
	if s.pages >= decayPages {
		t.Fatalf("pages = %d, want it halved below %d", s.pages, decayPages)
	}
	if got := s.counts["old"].count; got > decayPages/2 {
		t.Fatalf("count = %d, want it halved", got)
	}

	// The old chrome stops appearing; new chrome starts. Without decay the
	// old signature would outrank the new one indefinitely.
	for i := 0; i < decayPages; i++ {
		s.Observe([]string{"new"})
	}
	if s.isBoilerplate("old") {
		t.Error("chrome that stopped appearing is still treated as boilerplate")
	}
	if !s.isBoilerplate("new") {
		t.Error("chrome that replaced it was not learned")
	}
}

func TestEvictionKeepsEstablishedSignatures(t *testing.T) {
	s := ParseState(nil)
	s.pages = 4
	s.counts["chrome"] = sigCount{count: 4, lastSeen: 4}
	for i := 0; i < 50; i++ {
		s.counts[fmt.Sprintf("once-%02d", i)] = sigCount{count: 1, lastSeen: i}
	}

	s.evictTo(10)

	if len(s.counts) != 10 {
		t.Fatalf("counts = %d, want 10", len(s.counts))
	}
	if !s.isBoilerplate("chrome") {
		t.Error("established chrome signature was evicted before one-off signatures")
	}
	// Ties on count fall back to last-seen, so the survivors are the most
	// recent one-offs -- and always the same ones, whatever order the map
	// iterated in.
	if _, ok := s.counts["once-49"]; !ok {
		t.Error("most recently seen one-off was evicted before older ones")
	}
	if _, ok := s.counts["once-00"]; ok {
		t.Error("oldest one-off survived eviction")
	}
}
