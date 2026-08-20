// Package boilerplate strips the parts of an article page that the same
// feed's other article pages also carry -- site navigation, headers,
// footers, sidebars -- before the page is handed to Readability
// (internal/fulltext).
//
// Readability picks the article body by scoring candidate nodes, which
// breaks down on pages whose real content has no container of its own (a
// bare <pre> under <body>, table-based layouts, tag soup with omitted end
// tags): the top candidate ends up being <body> itself and the whole site
// chrome comes back as "the article". Feedla fetches many article pages of
// the same feed over time, so anything that repeats across them is chrome by
// construction, whatever the page's markup looks like.
//
// The package is pure: no DB, no HTTP. Callers keep the per-feed State
// (opaque JSON) and hand it back on the next page.
package boilerplate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/tokuhirom/feedla/internal/extract"
)

const (
	// StateVersion is bumped whenever the signature function or the State
	// encoding changes in a way that invalidates stored counts. ParseState
	// treats any other version as "no state yet", which costs a few crawls
	// of re-learning and nothing else.
	StateVersion = 1

	// minSignatureTextLen is the shortest normalized text a subtree may
	// carry and still be recorded or removed. Without it every leaf on the
	// page ("Home", a bullet, a bare <br>) would take a slot in State, and
	// the boilerplate candidates that matter -- whole nav blocks -- would be
	// evicted before their second sighting. It also keeps single short
	// phrases that legitimately repeat across articles (a byline, a section
	// label) from being cut out of the body.
	minSignatureTextLen = 32

	// maxSignatures caps how many signatures a feed's State keeps.
	maxSignatures = 2000

	// decayPages is the observed-page count at which every counter is
	// halved (see State.Observe). This bounds Pages, so the ratio rule in
	// isBoilerplate keeps working after a site redesign: chrome that
	// stopped appearing decays out instead of sitting at a high count
	// forever, and the replacement chrome only has to out-count a bounded
	// window rather than the feed's whole history.
	decayPages = 64

	// minPages is the fewest sightings that can make a subtree boilerplate.
	// Two is the minimum that means "repeats at all"; the ratio rule below
	// is what keeps it from firing on a block that merely happens to appear
	// in two articles out of many.
	minPages = 2
)

// State is one feed's accumulated view of which subtrees repeat across its
// article pages. The zero value is not usable -- callers get a State from
// ParseState (which returns a ready-to-use empty one for nil/invalid
// input).
type State struct {
	// pages is how many article pages have been observed, halved on decay.
	pages int
	// counts maps a subtree signature to how many observed pages carried
	// it, plus the page ordinal it was last seen on (used only to break
	// eviction ties deterministically).
	counts map[string]sigCount
}

type sigCount struct {
	count    int
	lastSeen int
}

// stateJSON is State's wire format. Counts are stored as [count, lastSeen]
// pairs to keep the JSON compact: a full State is ~2000 entries.
type stateJSON struct {
	Version int               `json:"v"`
	Pages   int               `json:"pages"`
	Counts  map[string][2]int `json:"counts"`
}

// ParseState decodes raw, which may be nil, empty, malformed, or written by
// a different StateVersion -- all of which yield an empty State rather than
// an error. Corrupt state here is never worth failing a crawl over: the
// worst case is that this feed's pages go through Readability unstripped,
// exactly as they did before this package existed.
func ParseState(raw json.RawMessage) *State {
	s := &State{counts: map[string]sigCount{}}
	if len(raw) == 0 {
		return s
	}
	var sj stateJSON
	if err := json.Unmarshal(raw, &sj); err != nil {
		return s
	}
	if sj.Version != StateVersion {
		return s
	}
	s.pages = sj.Pages
	for sig, pair := range sj.Counts {
		if pair[0] <= 0 {
			continue
		}
		s.counts[sig] = sigCount{count: pair[0], lastSeen: pair[1]}
	}
	return s
}

// Marshal encodes s for storage.
func (s *State) Marshal() (json.RawMessage, error) {
	sj := stateJSON{
		Version: StateVersion,
		Pages:   s.pages,
		Counts:  make(map[string][2]int, len(s.counts)),
	}
	for sig, c := range s.counts {
		sj.Counts[sig] = [2]int{c.count, c.lastSeen}
	}
	return json.Marshal(sj)
}

// Pages reports how many article pages this State has observed (post-decay,
// so it is bounded by decayPages). Callers use it for logging.
func (s *State) Pages() int { return s.pages }

// isBoilerplate reports whether sig has repeated often enough to be treated
// as site chrome. Both halves matter: count >= minPages rejects
// first-sighting subtrees, and count*2 >= pages rejects a block shared by a
// handful of articles out of many -- a serialized post's shared intro
// paragraph, a recurring license header -- which is body text that happens
// to repeat, not chrome. Real chrome is on every page, so it clears the
// ratio comfortably.
func (s *State) isBoilerplate(sig string) bool {
	c, ok := s.counts[sig]
	if !ok {
		return false
	}
	return c.count >= minPages && c.count*2 >= s.pages
}

// Apply removes every known-boilerplate subtree from doc and returns how
// many nodes it removed along with the signatures to hand to Observe.
//
// Removal and observation are one traversal on purpose. A subtree that gets
// removed is still reported (so its count keeps rising and it stays
// boilerplate), but its descendants are not visited at all: once the whole
// nav block is gone, its inner lists are not worth a slot in State. That is
// what keeps a page's thousands of nodes from crowding out the handful of
// signatures that do the work.
//
// The <head> subtree is never touched. Its children are identical across a
// site's pages by definition, and dropping <base href> would silently
// re-anchor every relative link and image Readability resolves, with no
// change in text length for the caller's length check to catch.
func Apply(doc *html.Node, s *State) (removed int, sigs []string) {
	digests := map[*html.Node]nodeDigest{}
	computeDigests(doc, digests)

	seen := map[string]bool{}
	record := func(sig string) {
		if !seen[sig] {
			seen[sig] = true
			sigs = append(sigs, sig)
		}
	}

	var walk func(parent *html.Node)
	walk = func(parent *html.Node) {
		var next *html.Node
		for c := parent.FirstChild; c != nil; c = next {
			next = c.NextSibling
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "head" {
				continue
			}
			d := digests[c]
			// html/body are skipped as candidates (removing either
			// removes the page) but still walked into.
			if c.Data != "html" && c.Data != "body" && d.textLen >= minSignatureTextLen {
				record(d.sig)
				if s.isBoilerplate(d.sig) {
					parent.RemoveChild(c)
					removed++
					continue
				}
			}
			walk(c)
		}
	}
	walk(doc)
	return removed, sigs
}

// Observe folds one page's signatures into s. Callers pass what Apply
// returned, i.e. the signatures of the page as fetched, including subtrees
// Apply just removed -- counting only what survives would let a block's
// count stall the moment it starts being removed.
func (s *State) Observe(sigs []string) {
	s.pages++
	for _, sig := range sigs {
		c := s.counts[sig]
		c.count++
		c.lastSeen = s.pages
		s.counts[sig] = c
	}
	if s.pages >= decayPages {
		s.decay()
	}
	s.evict()
}

// decay halves every counter, dropping the ones that reach zero. Sightings
// thus age out geometrically instead of accumulating forever, which is what
// lets a redesigned site converge on its new chrome.
func (s *State) decay() {
	for sig, c := range s.counts {
		c.count /= 2
		if c.count == 0 {
			delete(s.counts, sig)
			continue
		}
		s.counts[sig] = c
	}
	s.pages /= 2
}

func (s *State) evict() { s.evictTo(maxSignatures) }

// evictTo trims counts back to limit, dropping the least-established
// signatures first: lowest count, then least recently seen, then by
// signature so the outcome never depends on map iteration order.
func (s *State) evictTo(limit int) {
	if len(s.counts) <= limit {
		return
	}
	sigs := make([]string, 0, len(s.counts))
	for sig := range s.counts {
		sigs = append(sigs, sig)
	}
	sort.Slice(sigs, func(i, j int) bool {
		a, b := s.counts[sigs[i]], s.counts[sigs[j]]
		if a.count != b.count {
			return a.count < b.count
		}
		if a.lastSeen != b.lastSeen {
			return a.lastSeen < b.lastSeen
		}
		return sigs[i] < sigs[j]
	})
	for _, sig := range sigs[:len(s.counts)-limit] {
		delete(s.counts, sig)
	}
}

// nodeDigest is one node's subtree signature and the length of the
// normalized text under it.
type nodeDigest struct {
	sig     string
	textLen int
}

// computeDigests fills out with a digest for every node in doc, bottom-up
// in a single pass. A node's signature covers its tag, its attributes, and
// the signatures of its children, so two subtrees share a signature exactly
// when they render the same markup and text -- computed in O(nodes) rather
// than by re-serializing each subtree, which would be O(nodes * depth) and
// pathological on the deeply nested tag soup this package exists for.
func computeDigests(n *html.Node, out map[*html.Node]nodeDigest) nodeDigest {
	switch n.Type {
	case html.TextNode:
		text := extract.NormalizeText(n.Data)
		d := nodeDigest{sig: text, textLen: len([]rune(text))}
		out[n] = d
		return d
	case html.ElementNode:
		var b strings.Builder
		b.WriteString(n.Data)
		b.WriteByte(0)
		attrs := make([]string, 0, len(n.Attr))
		for _, a := range n.Attr {
			attrs = append(attrs, a.Key+"="+a.Val)
		}
		sort.Strings(attrs)
		b.WriteString(strings.Join(attrs, "\x00"))
		textLen := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			d := computeDigests(c, out)
			b.WriteByte(0)
			b.WriteString(d.sig)
			textLen += d.textLen
		}
		sum := sha256.Sum256([]byte(b.String()))
		d := nodeDigest{sig: hex.EncodeToString(sum[:8]), textLen: textLen}
		out[n] = d
		return d
	default:
		// Document, comment, doctype: no signature of their own, but
		// their children still need digests (the document node is where
		// the traversal starts).
		var b strings.Builder
		textLen := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			d := computeDigests(c, out)
			b.WriteByte(0)
			b.WriteString(d.sig)
			textLen += d.textLen
		}
		sum := sha256.Sum256([]byte(b.String()))
		d := nodeDigest{sig: hex.EncodeToString(sum[:8]), textLen: textLen}
		out[n] = d
		return d
	}
}
