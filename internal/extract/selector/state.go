package selector

import "encoding/json"

const CurrentStateVersion = 1

// State is the opaque JSON persisted in scrape_sources.state between crawls
// (§6.1). Unlike pagewatch, this is not "previous page content" but a
// history of which article URLs have already been imported.
type State struct {
	Version    int            `json:"version"`
	ConfigHash string         `json:"config_hash,omitempty"`
	Truncated  bool           `json:"truncated,omitempty"`
	Seen       []string       `json:"seen"`
	Pending    map[string]int `json:"pending,omitempty"`
}

// parseState decodes raw into a State. ok is false for a missing, corrupt,
// or unknown-version input, so the caller can fall back to resync mode
// (§6.3) instead of failing the whole Extract call.
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

func (s State) marshal() json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		panic("selector: marshal state: " + err.Error()) // unreachable: State fields are all marshalable
	}
	return b
}

// CommitInput is what crawler reports back after acting on one crawl's
// worth of new candidates (§6.5). It is used to update seen/pending; the
// extraction step itself (selector.Extract) never sees which fetches
// succeeded, since that requires HTTP.
type CommitInput struct {
	// Candidates is every new candidate URL from this crawl, in document
	// order, including ones crawler chose not to fetch this time.
	Candidates []string
	// Imported is the subset of Candidates an entry was created for —
	// whether from a successful article fetch, a fetch that failed 3 times
	// (§4.5), or a robots.txt Disallow.
	Imported []string
	// FetchFailed is the subset of Candidates whose article fetch failed
	// this crawl (and was not yet imported title-only).
	FetchFailed []string
	// Initial is true when prev was empty (state.go's "present" was false):
	// the very first crawl of this source.
	Initial bool
	// Truncated is true when item_selector matched more than MaxCandidates
	// elements this crawl.
	Truncated bool
	// ConfigHash is the current config's hash, stored for display only.
	ConfigHash string
}

// CommitState is a pure function (no HTTP, no DB) that folds one crawl's
// results into the next scrape_sources.state (§6.5's five rules).
func CommitState(prev json.RawMessage, in CommitInput) json.RawMessage {
	prevState, _ := parseState(prev)

	seenSet := make(map[string]bool, len(prevState.Seen)+len(in.Imported))
	order := make([]string, 0, len(prevState.Seen)+len(in.Imported))
	add := func(url string) {
		if !seenSet[url] {
			seenSet[url] = true
			order = append(order, url)
		}
	}
	for _, u := range prevState.Seen {
		add(u)
	}

	// Rule 1: Imported goes into seen.
	for _, u := range in.Imported {
		add(u)
	}

	// Rule 3: on the initial crawl, candidates that weren't even attempted
	// this time (i.e. beyond max_items_per_crawl) are sealed into seen too,
	// so a large backlog isn't slowly imported over following crawls.
	if in.Initial {
		failed := make(map[string]bool, len(in.FetchFailed))
		for _, u := range in.FetchFailed {
			failed[u] = true
		}
		for _, u := range in.Candidates {
			if !failed[u] {
				add(u)
			}
		}
	}

	candSet := make(map[string]bool, len(in.Candidates))
	for _, u := range in.Candidates {
		candSet[u] = true
	}

	// Rule 4: pending entries for URLs no longer among this crawl's
	// candidates are dropped (§6.1.2) — they're either imported/seen
	// already, or have fallen off the list page entirely.
	newPending := make(map[string]int, len(prevState.Pending))
	for u, n := range prevState.Pending {
		if candSet[u] {
			newPending[u] = n
		}
	}
	// A URL that got imported this crawl is no longer pending, regardless
	// of what rule 4 retained for it.
	for _, u := range in.Imported {
		delete(newPending, u)
	}
	// Rule 2: fetch failures increment their retry count.
	for _, u := range in.FetchFailed {
		newPending[u]++
	}
	if len(newPending) == 0 {
		newPending = nil
	}

	// Rule 5: once over the cap, evict the oldest entries that are NOT
	// among this crawl's candidates — an entry the list page still shows
	// must not be evicted, or it would look "new" again next crawl even
	// though its entries row may have been GC'd (§6.1.1).
	if len(order) > MaxSeen {
		toRemove := len(order) - MaxSeen
		kept := make([]string, 0, len(order))
		removed := 0
		for _, u := range order {
			if removed < toRemove && !candSet[u] {
				removed++
				continue
			}
			kept = append(kept, u)
		}
		order = kept
	}

	return State{
		Version:    CurrentStateVersion,
		ConfigHash: in.ConfigHash,
		Truncated:  in.Truncated,
		Seen:       order,
		Pending:    newPending,
	}.marshal()
}
