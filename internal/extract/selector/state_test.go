package selector

import (
	"encoding/json"
	"strconv"
	"testing"
)

func urlSet(t *testing.T, urls []string) map[string]bool {
	t.Helper()
	m := map[string]bool{}
	for _, u := range urls {
		m[u] = true
	}
	return m
}

func TestCommitStateImportedAndFetchFailed(t *testing.T) {
	prev, _ := json.Marshal(State{Version: CurrentStateVersion})
	raw := CommitState(prev, CommitInput{
		Candidates:  []string{"a", "b"},
		Imported:    []string{"a"},
		FetchFailed: []string{"b"},
	})
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !urlSet(t, st.Seen)["a"] {
		t.Errorf("Seen should contain imported url a: %v", st.Seen)
	}
	if urlSet(t, st.Seen)["b"] {
		t.Errorf("Seen should not contain failed url b: %v", st.Seen)
	}
	if st.Pending["b"] != 1 {
		t.Errorf("Pending[b] = %d, want 1", st.Pending["b"])
	}
}

func TestCommitStateThirdFailureGivesUp(t *testing.T) {
	prev, _ := json.Marshal(State{Version: CurrentStateVersion, Pending: map[string]int{"b": 2}})
	raw := CommitState(prev, CommitInput{
		Candidates: []string{"b"},
		Imported:   []string{"b"}, // caller decides to import title-only once retries are exhausted (§4.5)
	})
	var st State
	_ = json.Unmarshal(raw, &st)
	if !urlSet(t, st.Seen)["b"] {
		t.Errorf("b should be in Seen once imported title-only: %v", st.Seen)
	}
	if _, stillPending := st.Pending["b"]; stillPending {
		t.Errorf("b should no longer be pending: %v", st.Pending)
	}
}

func TestCommitStateInitialSealsUnattemptedCandidates(t *testing.T) {
	prev, _ := json.Marshal(State{Version: CurrentStateVersion})
	raw := CommitState(prev, CommitInput{
		Candidates: []string{"a", "b", "c"},
		Imported:   []string{"a"},
		Initial:    true,
	})
	var st State
	_ = json.Unmarshal(raw, &st)
	set := urlSet(t, st.Seen)
	for _, u := range []string{"a", "b", "c"} {
		if !set[u] {
			t.Errorf("initial crawl should seal %q into Seen: %v", u, st.Seen)
		}
	}
}

func TestCommitStateNonInitialLeavesUnattemptedCandidatesUnsealed(t *testing.T) {
	prev, _ := json.Marshal(State{Version: CurrentStateVersion})
	raw := CommitState(prev, CommitInput{
		Candidates: []string{"a", "b", "c"},
		Imported:   []string{"a"},
		Initial:    false,
	})
	var st State
	_ = json.Unmarshal(raw, &st)
	set := urlSet(t, st.Seen)
	if !set["a"] {
		t.Errorf("a should be seen: %v", st.Seen)
	}
	if set["b"] || set["c"] {
		t.Errorf("non-initial crawl should leave unattempted candidates for next crawl: %v", st.Seen)
	}
}

func TestCommitStatePendingCleanupForStaleCandidates(t *testing.T) {
	prev, _ := json.Marshal(State{Version: CurrentStateVersion, Pending: map[string]int{"stale": 2, "still-here": 1}})
	raw := CommitState(prev, CommitInput{
		Candidates:  []string{"still-here"}, // "stale" fell off the listing page
		FetchFailed: []string{"still-here"},
	})
	var st State
	_ = json.Unmarshal(raw, &st)
	if _, ok := st.Pending["stale"]; ok {
		t.Errorf("stale pending entry should be dropped: %v", st.Pending)
	}
	if st.Pending["still-here"] != 2 {
		t.Errorf("still-here pending = %d, want 2 (1 prior + 1 this crawl)", st.Pending["still-here"])
	}
}

func TestCommitStateSeenEvictionSparesCurrentCandidates(t *testing.T) {
	// Seed Seen at the cap: N old entries plus one that's still on the
	// listing page (a candidate this crawl), oldest-first.
	seen := make([]string, 0, MaxSeen)
	seen = append(seen, "still-on-page")
	for i := 1; i < MaxSeen; i++ {
		seen = append(seen, "old-"+strconv.Itoa(i))
	}
	prev, _ := json.Marshal(State{Version: CurrentStateVersion, Seen: seen})

	raw := CommitState(prev, CommitInput{
		Candidates: []string{"still-on-page", "new-article"},
		Imported:   []string{"new-article"},
	})
	var st State
	_ = json.Unmarshal(raw, &st)
	if len(st.Seen) > MaxSeen {
		t.Fatalf("Seen exceeds cap: %d", len(st.Seen))
	}
	if !urlSet(t, st.Seen)["still-on-page"] {
		t.Error("an entry still on the listing page must never be evicted")
	}
	if !urlSet(t, st.Seen)["new-article"] {
		t.Error("newly imported entry should be present")
	}
	if urlSet(t, st.Seen)["old-1"] {
		t.Error("the oldest non-candidate entry should have been evicted first")
	}
}

func TestCommitStateConfigChangeKeepsSeen(t *testing.T) {
	prev, _ := json.Marshal(State{Version: CurrentStateVersion, ConfigHash: "old-hash", Seen: []string{"a"}})
	raw := CommitState(prev, CommitInput{
		Candidates: []string{"a"},
		ConfigHash: "new-hash",
	})
	var st State
	_ = json.Unmarshal(raw, &st)
	if !urlSet(t, st.Seen)["a"] {
		t.Error("changing config_hash must not drop existing Seen entries")
	}
	if st.ConfigHash != "new-hash" {
		t.Errorf("ConfigHash = %q, want new-hash", st.ConfigHash)
	}
}
