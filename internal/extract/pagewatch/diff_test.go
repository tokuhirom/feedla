package pagewatch

import (
	"fmt"
	"testing"
)

func TestComputeDiff_Basic(t *testing.T) {
	prev := []StateBlock{{Key: "A"}, {Key: "B"}}
	curr := []Block{{Key: "A"}, {Key: "B"}, {Key: "C", HTML: "<p>C</p>"}}
	added, removed := computeDiff(prev, curr)
	if len(added) != 1 || added[0].Key != "C" {
		t.Fatalf("added = %+v, want exactly one block with Key C", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %+v, want none", removed)
	}
}

func TestComputeDiff_ReorderOnlyCancelsOut(t *testing.T) {
	prev := []StateBlock{{Key: "A"}, {Key: "B"}, {Key: "C"}}
	curr := []Block{{Key: "C"}, {Key: "A"}, {Key: "B"}}
	added, removed := computeDiff(prev, curr)
	if len(added) != 0 || len(removed) != 0 {
		t.Errorf("reordered-only diff: added=%+v removed=%+v, want none (§4.6: matching keys cancel across added/removed)", added, removed)
	}
}

func TestComputeDiff_LargePageUsesSetDiffFallback(t *testing.T) {
	n := 2500
	prev := make([]StateBlock, n)
	curr := make([]Block, n+1)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("block-%d", i)
		prev[i] = StateBlock{Key: key}
		curr[i] = Block{Key: key}
	}
	curr[n] = Block{Key: "new-block", HTML: "<p>new</p>"}

	added, removed := computeDiff(prev, curr)
	if len(added) != 1 || added[0].Key != "new-block" {
		t.Fatalf("added = %+v, want exactly the new block", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %+v, want none", removed)
	}
}
