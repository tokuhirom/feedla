// Small orchestration layer that ties subscriptions + entries state
// together, so components (and the keyboard handler) call one function
// instead of sequencing loadEntries/prefetchNext by hand.
import * as api from '../api/client'
import { entries, focusedIndex, loadEntries, prefetchNext } from './entries'
import { pins } from './pins'
import { loadSubscriptions, removeSubscription, selectedFeedId, selectFeed, subscriptions } from './subscriptions'
import { showToast } from './ui'

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

// Unsubscribes feedId regardless of whether it's the currently selected
// feed (the ErrorFeedsOverlay unsubscribes feeds that aren't selected).
// Confirms first -- unsubscribing cascades to the feed's entries/pins
// server-side and can't be undone short of re-subscribing from scratch,
// and the header's ✕ button sits right next to the refresh/nav buttons a
// reader taps constantly, so a stray tap shouldn't be irreversible.
export async function unsubscribeFeed(feedId: number): Promise<void> {
  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  const label = sub?.title || sub?.feed_url || 'このフィード'
  if (!window.confirm(`「${label}」の購読を解除しますか？\n記事・pin も削除され、元に戻せません。`)) {
    return
  }

  const wasSelected = selectedFeedId.value === feedId
  await api.deleteSubscription(feedId)
  removeSubscription(feedId)
  if (wasSelected) {
    entries.value = []
  }
}

export async function unsubscribeCurrentFeed(): Promise<void> {
  const feedId = selectedFeedId.value
  if (feedId === null) return
  await unsubscribeFeed(feedId)
}

// Toggles pin state on the keyboard-focused entry (the `p` shortcut),
// optimistically flipping Entry.pinned and rolling back on failure.
export async function togglePinFocused(): Promise<void> {
  const entry = entries.value[focusedIndex.value]
  if (!entry) return
  const wasPinned = entry.pinned

  entries.value = entries.value.map((e) => (e.id === entry.id ? { ...e, pinned: !wasPinned } : e))
  try {
    if (wasPinned) {
      await api.removePin(entry.id)
      pins.value = pins.value.filter((p) => p.entry_id !== entry.id)
      showToast('pin を解除しました')
    } else {
      await api.addPin(entry.id)
      showToast('pin しました')
    }
  } catch (e) {
    entries.value = entries.value.map((ee) => (ee.id === entry.id ? { ...ee, pinned: wasPinned } : ee))
    showToast(e instanceof Error ? e.message : String(e))
  }
}
