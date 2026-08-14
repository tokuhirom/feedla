// Small orchestration layer that ties subscriptions + entries state
// together, so components (and the keyboard handler) call one function
// instead of sequencing loadEntries/prefetchNext by hand.
import * as api from '../api/client'
import { entries, loadEntries, prefetchNext } from './entries'
import { loadSubscriptions, removeSubscription, selectedFeedId, selectFeed } from './subscriptions'

export async function selectAndLoadFeed(feedId: number): Promise<void> {
  selectFeed(feedId)
  await loadEntries(feedId)
  void prefetchNext()
}

// Re-crawls the current feed on the server (this is what README's `r` key
// maps to -- see the Phase4 plan for why `z` was folded into it) and then
// reloads its unread list and subscription counts.
export async function refreshCurrentFeed(): Promise<void> {
  const feedId = selectedFeedId.value
  if (feedId === null) return
  await api.refreshSubscription(feedId)
  await loadSubscriptions()
  await loadEntries(feedId)
}

export async function unsubscribeCurrentFeed(): Promise<void> {
  const feedId = selectedFeedId.value
  if (feedId === null) return
  await api.deleteSubscription(feedId)
  removeSubscription(feedId)
  entries.value = []
}
