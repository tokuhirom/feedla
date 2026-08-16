package pagewatch

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseState_MissingOrCorruptOrWrongVersion(t *testing.T) {
	cases := []json.RawMessage{
		nil,
		[]byte(``),
		[]byte(`{not json`),
		[]byte(`{"version":2}`),
	}
	for _, raw := range cases {
		if _, ok := parseState(raw); ok {
			t.Errorf("parseState(%q) = ok, want not ok", raw)
		}
	}
}

func TestState_MarshalDropsDisplayHTMLWhenOversized(t *testing.T) {
	blocks := make([]StateBlock, 2000)
	for i := range blocks {
		blocks[i] = StateBlock{Key: strings.Repeat("k", 50), HTML: strings.Repeat("h", 500)}
	}
	st := State{Version: CurrentStateVersion, RulesVersion: CurrentRulesVersion, Blocks: blocks}
	raw := st.marshal()
	if len(raw) > MaxStateBytes {
		t.Fatalf("marshaled state = %d bytes, want <= %d after dropping display HTML", len(raw), MaxStateBytes)
	}

	var decoded State
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for i, b := range decoded.Blocks {
		if b.HTML != "" {
			t.Fatalf("block %d still has display HTML after size-triggered drop", i)
		}
		if b.Key == "" {
			t.Fatalf("block %d lost its comparison key", i)
		}
	}
}

func TestState_MarshalKeepsDisplayHTMLWhenSmall(t *testing.T) {
	st := State{
		Version:      CurrentStateVersion,
		RulesVersion: CurrentRulesVersion,
		Blocks:       []StateBlock{{Key: "k1", HTML: "<p>h1</p>"}},
	}
	raw := st.marshal()
	var decoded State
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Blocks[0].HTML != "<p>h1</p>" {
		t.Errorf("display HTML was dropped even though state is well under the size cap: %+v", decoded.Blocks[0])
	}
}

func TestRecomputeStateBlocks_MaskingAndDroppedHTML(t *testing.T) {
	patterns, err := (Config{IgnorePatterns: []string{"noisy"}}).compiledIgnorePatterns()
	if err != nil {
		t.Fatalf("compiledIgnorePatterns: %v", err)
	}

	t.Run("remasks and drops emptied blocks", func(t *testing.T) {
		prev := []StateBlock{
			{Key: "old-key-1", HTML: "<p>本文です。</p>"},
			{Key: "old-key-2", HTML: "<p>noisy</p>"},
		}
		out, ok := recomputeStateBlocks(prev, patterns)
		if !ok {
			t.Fatal("recomputeStateBlocks: want ok, since all display HTML is present")
		}
		if len(out) != 1 || out[0].HTML != "<p>本文です。</p>" {
			t.Fatalf("out = %+v, want the noisy-only block dropped", out)
		}
	})

	t.Run("fails when display HTML was dropped for size", func(t *testing.T) {
		prev := []StateBlock{{Key: "old-key", HTML: ""}}
		if _, ok := recomputeStateBlocks(prev, patterns); ok {
			t.Error("recomputeStateBlocks: want not ok when a block's display HTML is empty")
		}
	})
}
