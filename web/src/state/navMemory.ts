/** Where `a` (前の購読へ) gets its notion of "the feed I was just reading"
 * from. See docs/keyboard-shortcuts.md for the full behavior spec.
 *
 * Two pieces of memory, both deliberately in-memory only (no localStorage):
 *
 * - visitedFeedIds -- which feeds this tab has actually opened. `a` needs
 *   this because a feed the reader just finished with has unread_count 0,
 *   and adjacentFeedId's plain "skip fully-read feeds" rule would step
 *   straight over it to the one before.
 * - lastFocusedEntry -- where in each feed the reader was, so coming back
 *   lands on the entry they left off at rather than the top of the list.
 *
 * Persisting either one would make `a` worse, not better: a visited set
 * that survives restarts eventually contains every feed, at which point
 * `a` degenerates into a plain "one feed up in the sidebar" key.
 */

const visitedFeedIds = new Set<number>()
const lastFocusedEntry = new Map<number, number>()

export function markFeedVisited(feedId: number): void {
  visitedFeedIds.add(feedId)
}

export function hasVisitedFeed(feedId: number): boolean {
  return visitedFeedIds.has(feedId)
}

export function rememberFocusedEntry(feedId: number, entryId: number): void {
  lastFocusedEntry.set(feedId, entryId)
}

export function recallFocusedEntry(feedId: number): number | undefined {
  return lastFocusedEntry.get(feedId)
}

export function forgetFeed(feedId: number): void {
  visitedFeedIds.delete(feedId)
  lastFocusedEntry.delete(feedId)
}

/** Test-only: module-level state that would otherwise leak between cases. */
export function resetNavMemory(): void {
  visitedFeedIds.clear()
  lastFocusedEntry.clear()
}
