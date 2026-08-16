package pagewatch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

const (
	CurrentStateVersion = 1
	CurrentRulesVersion = 1

	MaxStateBytes = 512 * 1024
	MaxBlocks     = 5000
)

// State is the opaque JSON persisted in scrape_sources.state between crawls.
type State struct {
	Version      int          `json:"version"`
	RulesVersion int          `json:"rules_version"`
	ConfigHash   string       `json:"config_hash"`
	ContentHash  string       `json:"content_hash"`
	Truncated    bool         `json:"truncated"` // block-count cap hit (§4.4)
	Blocks       []StateBlock `json:"blocks"`
}

// StateBlock is one comparable block as persisted. HTML is empty when it was
// dropped to keep the state under MaxStateBytes (§6.3); Key/HTML/Anchor/Head
// mirror Block's fields of the same name.
type StateBlock struct {
	Key    string `json:"k"`
	HTML   string `json:"h,omitempty"`
	Anchor string `json:"a,omitempty"`
	Head   string `json:"t,omitempty"`
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hashKeys(keys []string) string {
	return sha256Hex([]byte(strings.Join(keys, "\n")))
}

func contentHashBlocks(blocks []Block) string {
	keys := make([]string, len(blocks))
	for i, b := range blocks {
		keys[i] = b.Key
	}
	return hashKeys(keys)
}

func contentHashStateBlocks(blocks []StateBlock) string {
	keys := make([]string, len(blocks))
	for i, b := range blocks {
		keys[i] = b.Key
	}
	return hashKeys(keys)
}

func blocksToStateBlocks(blocks []Block) []StateBlock {
	out := make([]StateBlock, len(blocks))
	for i, b := range blocks {
		out[i] = StateBlock{Key: b.Key, HTML: b.HTML, Anchor: b.Anchor, Head: b.Head}
	}
	return out
}

// parseState decodes raw into a State. ok is false for a missing, corrupt,
// or unknown-version input, so the caller can fall back to resync mode
// (§6.6) instead of failing the whole Extract call.
func parseState(raw json.RawMessage) (st State, ok bool) {
	if len(raw) == 0 {
		return State{}, false
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, false
	}
	if st.Version != CurrentStateVersion {
		return State{}, false
	}
	return st, true
}

// marshal serializes s, dropping all Blocks[].HTML (display HTML) if the
// full encoding would exceed MaxStateBytes (§6.3). Comparison keys (k) are
// always kept; only the display HTML used to render removed blocks is
// sacrificed.
func (s State) marshal() json.RawMessage {
	b := mustMarshalJSON(s)
	if len(b) <= MaxStateBytes {
		return b
	}
	stripped := s
	stripped.Blocks = make([]StateBlock, len(s.Blocks))
	for i, blk := range s.Blocks {
		blk.HTML = ""
		stripped.Blocks[i] = blk
	}
	return mustMarshalJSON(stripped)
}

func mustMarshalJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("pagewatch: marshal state: " + err.Error()) // unreachable: State fields are all marshalable
	}
	return b
}

// recomputeStateBlocks remasks prev's saved display HTML with the current
// ignore_patterns, dropping blocks that mask to empty text. It reports
// ok=false if any block's display HTML was previously dropped for size
// (§6.3), since remasking is then impossible and the caller must resync.
func recomputeStateBlocks(prev []StateBlock, patterns []*regexp.Regexp) ([]StateBlock, bool) {
	out := make([]StateBlock, 0, len(prev))
	for _, b := range prev {
		if b.HTML == "" {
			return nil, false
		}
		masked := applyIgnorePatterns(b.HTML, patterns)
		if stripTags(masked) == "" {
			continue
		}
		out = append(out, StateBlock{Key: masked, HTML: b.HTML, Anchor: b.Anchor, Head: b.Head})
	}
	return out, true
}
