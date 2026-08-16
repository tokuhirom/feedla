package pagewatch

import "sort"

// lcsBlockLimit: above this many blocks on either side, computeDiff falls
// back from LCS (O(n*m) cells) to a multiset diff to bound CPU/memory
// (§4.6). Order-change detection is lost, but that's an acceptable trade —
// a page that large is already at the block-count cap.
const lcsBlockLimit = 2000

// computeDiff compares the previous comparable block sequence against the
// current one and returns Added/Removed in document order.
func computeDiff(prev []StateBlock, curr []Block) (added, removed []Block) {
	prevKeys := make([]string, len(prev))
	for i, b := range prev {
		prevKeys[i] = b.Key
	}
	currKeys := make([]string, len(curr))
	for i, b := range curr {
		currKeys[i] = b.Key
	}

	var addedIdx, removedIdx []int
	if len(prevKeys) > lcsBlockLimit || len(currKeys) > lcsBlockLimit {
		addedIdx, removedIdx = setDiffIdx(prevKeys, currKeys)
	} else {
		addedIdx, removedIdx = lcsDiffIdx(prevKeys, currKeys)
	}
	// A block that only moved (present on both sides, just reordered) must
	// not read as a delete+add; cancel matching keys out of both sets. This
	// applies even on the LCS path, since LCS alignment can still split one
	// moved block into an add and a remove (§4.6).
	addedIdx, removedIdx = cancelCommonKeys(addedIdx, currKeys, removedIdx, prevKeys)

	for _, idx := range addedIdx {
		added = append(added, curr[idx])
	}
	for _, idx := range removedIdx {
		sb := prev[idx]
		removed = append(removed, Block{
			HTML:   sb.HTML,
			Text:   stripTags(sb.HTML),
			Key:    sb.Key,
			Anchor: sb.Anchor,
			Head:   sb.Head,
		})
	}
	return added, removed
}

// lcsDiffIdx computes a standard longest-common-subsequence alignment and
// returns the indices that fell out of the alignment on each side.
func lcsDiffIdx(prevKeys, currKeys []string) (addedIdx, removedIdx []int) {
	n, m := len(prevKeys), len(currKeys)
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case prevKeys[i] == currKeys[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	i, j := 0, 0
	for i < n && j < m {
		switch {
		case prevKeys[i] == currKeys[j]:
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			removedIdx = append(removedIdx, i)
			i++
		default:
			addedIdx = append(addedIdx, j)
			j++
		}
	}
	for ; i < n; i++ {
		removedIdx = append(removedIdx, i)
	}
	for ; j < m; j++ {
		addedIdx = append(addedIdx, j)
	}
	return addedIdx, removedIdx
}

// setDiffIdx computes a multiset (bag) difference: for each key, a surplus
// of occurrences in curr counts as added, a surplus in prev counts as
// removed, and equal counts on both sides cancel out entirely — so a block
// that merely moved (reordering only) produces no diff (§4.6).
func setDiffIdx(prevKeys, currKeys []string) (addedIdx, removedIdx []int) {
	prevPos := map[string][]int{}
	for i, k := range prevKeys {
		prevPos[k] = append(prevPos[k], i)
	}
	currPos := map[string][]int{}
	for i, k := range currKeys {
		currPos[k] = append(currPos[k], i)
	}
	seen := map[string]bool{}
	for _, k := range prevKeys {
		seen[k] = true
	}
	for _, k := range currKeys {
		seen[k] = true
	}
	for k := range seen {
		pi := prevPos[k]
		ci := currPos[k]
		net := len(ci) - len(pi)
		switch {
		case net > 0:
			addedIdx = append(addedIdx, ci[len(ci)-net:]...)
		case net < 0:
			removedIdx = append(removedIdx, pi[:-net]...)
		}
	}
	sort.Ints(addedIdx)
	sort.Ints(removedIdx)
	return addedIdx, removedIdx
}

// cancelCommonKeys drops indices from addedIdx/removedIdx whose keys occur
// on both sides, up to the shared count — the same "moved, not changed"
// cancellation setDiffIdx does natively, applied as a post-pass so the LCS
// path gets it too (§4.6).
func cancelCommonKeys(addedIdx []int, currKeys []string, removedIdx []int, prevKeys []string) ([]int, []int) {
	addCount := map[string]int{}
	for _, idx := range addedIdx {
		addCount[currKeys[idx]]++
	}
	remCount := map[string]int{}
	for _, idx := range removedIdx {
		remCount[prevKeys[idx]]++
	}
	cancel := map[string]int{}
	for k, ac := range addCount {
		if rc := remCount[k]; rc > 0 {
			if rc < ac {
				cancel[k] = rc
			} else {
				cancel[k] = ac
			}
		}
	}

	filter := func(idx []int, keys []string) []int {
		var out []int
		used := map[string]int{}
		for _, i := range idx {
			k := keys[i]
			if used[k] < cancel[k] {
				used[k]++
				continue
			}
			out = append(out, i)
		}
		return out
	}
	return filter(addedIdx, currKeys), filter(removedIdx, prevKeys)
}
