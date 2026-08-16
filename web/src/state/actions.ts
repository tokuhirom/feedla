// Small orchestration layer that ties subscriptions + entries state
// together, so components (and the keyboard handler) call one function
// instead of sequencing loadEntries/prefetchNext by hand.
import * as api from '../api/client'
import type { SubscriptionView } from '../api/types'
import {
  entries,
  focusedIndex,
  loadEntries,
  loadGroupEntries,
  loadSearchEntries,
  markVisibleEntriesRead,
  prefetchNext,
} from './entries'
import { pins } from './pins'
import {
  applySubscriptionPatch,
  feedManagerMode,
  type GroupTarget,
  groupTarget,
  isSameGroupTarget,
  loadSubscriptions,
  pushMobileDetailNav,
  removeSubscription,
  searchMode,
  searchQuery,
  selectedFeedId,
  selectFeed,
  subscriptions,
  todayUnreadCount,
} from './subscriptions'
import { feedManagerInitialOnlyErrors, showToast } from './ui'

export async function selectAndLoadFeed(feedId: number): Promise<void> {
  // Only mark on an actual switch away -- re-clicking the already-selected
  // feed's own row (AddSubscriptionDialog's post-subscribe selectFeed already
  // put it there) isn't a page transition and shouldn't mark anything read.
  if (selectedFeedId.value !== feedId) markVisibleEntriesRead()
  selectFeed(feedId)
  await loadEntries(feedId)
  void prefetchNext()
}

// Opens a sidebar group (a folder or a priority/★ level) as a single merged
// reading target -- the "read everything in this folder/level at once" UI.
export async function selectGroup(target: GroupTarget): Promise<void> {
  if (
    selectedFeedId.value !== null ||
    searchMode.value ||
    !isSameGroupTarget(groupTarget.value, target)
  )
    markVisibleEntriesRead()
  pushMobileDetailNav(null)
  selectedFeedId.value = null
  groupTarget.value = target
  searchMode.value = false
  feedManagerMode.value = false
  await loadGroupEntries(target)
}

// Switches the entry pane into search mode with an empty query, ready for
// typing -- the `/` shortcut and the sidebar header's ⋮ menu both open
// search this way (see SearchHeader for the actual input). Doesn't touch an
// already-open search (its query/results stay put; the user can just click
// back into the input).
export function openSearch(): void {
  if (searchMode.value) return
  markVisibleEntriesRead()
  pushMobileDetailNav(null)
  selectedFeedId.value = null
  groupTarget.value = null
  searchMode.value = true
  searchQuery.value = ''
  feedManagerMode.value = false
  entries.value = []
}

// Runs (or re-runs) a search, replacing the entry pane's contents with
// results across every feed -- same read/pin/scroll behavior as a normal
// feed or group, since it lands in the same `entries` signal (see
// loadSearchEntries).
export async function runSearch(query: string): Promise<void> {
  const trimmed = query.trim()
  if (!trimmed) return
  markVisibleEntriesRead()
  pushMobileDetailNav(null)
  selectedFeedId.value = null
  groupTarget.value = null
  searchMode.value = true
  searchQuery.value = trimmed
  feedManagerMode.value = false
  await loadSearchEntries(trimmed)
}

// Switches the entry pane into the feed management list (FeedManagerPane) --
// the sidebar's ⚠ badge and ⋮ menu's フィード管理 item both open it this
// way. onlyErrors seeds FeedManagerPane's own filter state on mount (see
// feedManagerInitialOnlyErrors in state/ui.ts) so the ⚠ badge can jump
// straight to the erroring-feeds view.
export function openFeedManager(onlyErrors: boolean): void {
  markVisibleEntriesRead()
  pushMobileDetailNav(null)
  selectedFeedId.value = null
  groupTarget.value = null
  searchMode.value = false
  feedManagerInitialOnlyErrors.value = onlyErrors
  feedManagerMode.value = true
}

// Forces an immediate re-crawl of feedId on the server, bypassing the
// scheduler's own interval -- shared by refreshCurrentFeed (the `r`
// shortcut, always the selected feed) and FeedManagerPane/
// FeedDetailOverlay's 再クロール button, which can target any feed
// regardless of what's currently selected. Only reloads entries when
// feedId is the one actually showing, so refreshing a feed from the
// manager list doesn't yank the reader away from whatever they're reading.
export async function refreshFeed(
  feedId: number,
): Promise<{ new_entries: number; error?: string }> {
  const res = await api.refreshSubscription(feedId)
  await loadSubscriptions()
  if (selectedFeedId.value === feedId) {
    await loadEntries(feedId)
  }
  return res
}

// Re-crawls the current feed on the server (this is what README's `r` key
// maps to -- see the Phase4 plan for why `z` was folded into it) and then
// reloads its unread list and subscription counts.
export async function refreshCurrentFeed(): Promise<void> {
  const feedId = selectedFeedId.value
  if (feedId === null) return
  await refreshFeed(feedId)
}

// Unsubscribes feedId regardless of whether it's the currently selected
// feed (FeedManagerPane unsubscribes feeds that aren't selected).
// Confirms first -- unsubscribing cascades to the feed's entries/pins
// server-side and can't be undone short of re-subscribing from scratch,
// and the header's ✕ button sits right next to the refresh/nav buttons a
// reader taps constantly, so a stray tap shouldn't be irreversible.
export async function unsubscribeFeed(feedId: number): Promise<void> {
  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  const label = sub?.title || sub?.feed_url || 'このフィード'
  if (
    !window.confirm(
      `「${label}」の購読を解除しますか？\n記事・pin も削除され、元に戻せません。`,
    )
  ) {
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

// Marks every unread entry of feedId read via the backend's bulk
// read_all endpoint (FeedDetailOverlay's 全て既読にする button), instead
// of looping markReadOptimistic per entry -- one request instead of N,
// and it also covers unread entries the client hasn't even fetched (the
// entry pane only loads the first 200). Confirms first since, unlike a
// single entry, there's no "mark unread" undo in the UI.
export async function markFeedReadAll(feedId: number): Promise<void> {
  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  if (!sub) return
  if (
    !window.confirm(
      `「${sub.title || sub.feed_url}」の未読 ${sub.unread_count} 件をすべて既読にしますか？`,
    )
  ) {
    return
  }

  await api.readAll(feedId, 0)
  applySubscriptionPatch({ ...sub, unread_count: 0 })
  if (selectedFeedId.value === feedId) {
    entries.value = []
  }
}

// Marks every unread entry across every feed read via the backend's
// read_all-equivalent bulk endpoint -- the Sidebar ⋮ menu's "すべて既読に
// する" entry. Same confirm-first rationale as markFeedReadAll above: there's
// no undo. Doesn't touch search results (searchMode), since those aren't an
// unread-only view and clearing them would just discard the search.
export async function markAllRead(): Promise<void> {
  const totalUnread = subscriptions.value.reduce(
    (sum, s) => sum + s.unread_count,
    0,
  )
  if (totalUnread === 0) return
  if (!window.confirm(`未読 ${totalUnread} 件をすべて既読にしますか？`)) {
    return
  }

  await api.markAllEntriesRead()
  subscriptions.value = subscriptions.value.map((s) => ({
    ...s,
    unread_count: 0,
  }))
  todayUnreadCount.value = 0
  if (!searchMode.value) {
    entries.value = []
  }
}

// Applies nextRating optimistically and persists it, guarding against rapid
// repeated calls (e.g. mashing the +/- shortcut) racing each other: PATCH
// responses/errors can resolve out of order, so a call only reconciles the
// signal (adopting the server response, or rolling back on failure) if its
// own nextRating is still the current value -- otherwise a newer call has
// already superseded it and this one's outcome is stale and skipped.
async function patchRating(
  sub: SubscriptionView,
  nextRating: number,
): Promise<void> {
  const feedId = sub.feed_id
  const prevRating = sub.rating
  const isStillCurrent = () =>
    subscriptions.value.find((s) => s.feed_id === feedId)?.rating === nextRating

  applySubscriptionPatch({ ...sub, rating: nextRating })
  try {
    const updated = await api.patchSubscription(feedId, { rating: nextRating })
    if (isStillCurrent()) applySubscriptionPatch(updated)
  } catch (e) {
    if (isStillCurrent()) {
      const sub = subscriptions.value.find((s) => s.feed_id === feedId)
      if (sub) applySubscriptionPatch({ ...sub, rating: prevRating })
    }
    showToast(e instanceof Error ? e.message : String(e))
  }
}

// Sets feedId's rating (the header's ★☆☆☆☆ row). Clicking the star that's
// already the current rating clears it back to 0 rather than re-setting the
// same value, so there's a way to unrate without a separate control.
export async function setRating(feedId: number, rating: number): Promise<void> {
  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  if (!sub) return
  const nextRating = sub.rating === rating ? 0 : rating
  await patchRating(sub, nextRating)
}

// Adjusts feedId's rating by delta (the +/- shortcuts), clamped to
// [0, 5] -- the same 0..5 range the header's ★☆☆☆☆ row edits directly.
export async function adjustRating(
  feedId: number,
  delta: number,
): Promise<void> {
  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  if (!sub) return
  const nextRating = Math.min(5, Math.max(0, sub.rating + delta))
  if (nextRating === sub.rating) return
  await patchRating(sub, nextRating)
}

// Moves feedId to a different folder (the detail overlay's カテゴリ select).
// nextFolderId null means "(未分類)" -- the API represents that as
// folder_id: 0 (see store.SubscriptionPatch), not omission/null, since a
// nil pointer there means "leave untouched".
export async function moveFeedToFolder(
  feedId: number,
  nextFolderId: number | null,
): Promise<void> {
  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  if (!sub) return
  const prevFolderId = sub.folder_id ?? null
  if (nextFolderId === prevFolderId) return
  const isStillCurrent = () =>
    (subscriptions.value.find((s) => s.feed_id === feedId)?.folder_id ??
      null) === nextFolderId

  applySubscriptionPatch({ ...sub, folder_id: nextFolderId ?? undefined })
  try {
    const updated = await api.patchSubscription(feedId, {
      folder_id: nextFolderId ?? 0,
    })
    if (isStillCurrent()) applySubscriptionPatch(updated)
  } catch (e) {
    if (isStillCurrent()) {
      const sub = subscriptions.value.find((s) => s.feed_id === feedId)
      if (sub)
        applySubscriptionPatch({ ...sub, folder_id: prevFolderId ?? undefined })
    }
    showToast(e instanceof Error ? e.message : String(e))
  }
}

// Toggles pin state on the keyboard-focused entry (the `p` shortcut),
// optimistically flipping Entry.pinned and rolling back on failure.
export async function togglePinFocused(): Promise<void> {
  const entry = entries.value[focusedIndex.value]
  if (!entry) return
  const wasPinned = entry.pinned

  entries.value = entries.value.map((e) =>
    e.id === entry.id ? { ...e, pinned: !wasPinned } : e,
  )
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
    entries.value = entries.value.map((ee) =>
      ee.id === entry.id ? { ...ee, pinned: wasPinned } : ee,
    )
    showToast(e instanceof Error ? e.message : String(e))
  }
}
