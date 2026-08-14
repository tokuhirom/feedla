import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { Folder, SubscriptionView } from '../api/types'

export const subscriptions = signal<SubscriptionView[]>([])
export const folders = signal<Folder[]>([])
export const selectedFeedId = signal<number | null>(null)
export const loadingSubscriptions = signal(false)

/** How SubscriptionTree groups the sidebar: by folder ("カテゴリ") or by the
 * LDR-style ★ rating ("プライオリティ"). Same toggle drives both the desktop
 * two-pane layout and the mobile single-pane sidebar view -- there's no
 * separate mobile control, since the sidebar itself is just shown/hidden by
 * CSS on narrow viewports (see .has-selected-feed in global.css). */
export const sidebarViewMode = signal<'folder' | 'priority'>('folder')

/** A sidebar group ("Tech" folder, or the ★★★★★ priority level) selected as
 * a single merged reading target -- lets you read through every feed in the
 * group at once instead of picking feeds one by one. Mutually exclusive
 * with selectedFeedId (see selectFeed/clearSelectedFeed). */
export type GroupTarget =
  | { kind: 'folder'; folderId: number | null; label: string }
  | { kind: 'rating'; rating: number; label: string }

export const groupTarget = signal<GroupTarget | null>(null)

export function subscriptionsInFolder(folderId: number | null): SubscriptionView[] {
  return subscriptions.value.filter((s) => (s.folder_id ?? null) === folderId)
}

export function subscriptionsWithRating(rating: number): SubscriptionView[] {
  return subscriptions.value.filter((s) => s.rating === rating)
}

export function groupUnreadCount(target: GroupTarget): number {
  const subs =
    target.kind === 'folder' ? subscriptionsInFolder(target.folderId) : subscriptionsWithRating(target.rating)
  return subs.reduce((sum, s) => sum + s.unread_count, 0)
}

export async function loadSubscriptions(): Promise<void> {
  loadingSubscriptions.value = true
  try {
    const [subsRes, foldersRes] = await Promise.all([api.listSubscriptions(), api.listFolders()])
    subscriptions.value = subsRes.subscriptions
    folders.value = foldersRes.folders
  } finally {
    loadingSubscriptions.value = false
  }
}

/** Matches the `max-width: 700px` breakpoint in global.css that switches
 * the sidebar/entry-pane between single-pane (mobile) and two-pane (wide)
 * layouts. Below it, list -> detail is a real "navigation" that needs a
 * history entry so the OS back gesture returns to the list instead of
 * leaving the app; above it both panes are always visible, so there's
 * nothing to navigate. */
function isMobileViewport(): boolean {
  return window.matchMedia('(max-width: 700px)').matches
}

function isInDetail(): boolean {
  return selectedFeedId.value !== null || groupTarget.value !== null
}

type NavState = { feedId: number | null }

/** Pushes (from the list) or replaces (already in a detail view, e.g.
 * switching feeds with prev/next) a mobile history entry marking that a
 * feed/group detail is showing, so the browser/edge-swipe back gesture
 * pops back to the subscription list (see popstate handling in main.tsx)
 * instead of navigating away from feedla entirely. Called by both
 * selectFeed below and selectGroup (state/actions.ts). feedId is recorded
 * only so a later "forward" gesture can restore a specific feed; group
 * entries push feedId: null and just fall back to the list on forward
 * navigation, since there's no group data to restore. */
export function pushMobileDetailNav(feedId: number | null): void {
  if (!isMobileViewport()) return
  const state: NavState = { feedId }
  if (isInDetail()) {
    window.history.replaceState(state, '')
  } else {
    window.history.pushState(state, '')
  }
}

export function selectFeed(feedId: number): void {
  pushMobileDetailNav(feedId)
  selectedFeedId.value = feedId
  groupTarget.value = null
}

/** Deselects the current feed or group, returning to the subscription list.
 * On narrow (mobile) viewports this is what the entry pane's "戻る" back
 * button does -- on wide viewports the sidebar is visible regardless.
 * On mobile, this goes through history.back() (rather than clearing the
 * signal directly) so it consumes the entry selectFeed/selectGroup pushed,
 * keeping the browser back stack in sync with the in-app navigation. */
export function clearSelectedFeed(): void {
  if (isMobileViewport() && isInDetail()) {
    window.history.back()
    return
  }
  selectedFeedId.value = null
  groupTarget.value = null
}

/** Order subscriptions are traversed with s/a: the flat API order, which
 * mirrors sort_order/feed_id from the store (see ListSubscriptionViews). */
export function adjacentFeedId(direction: 1 | -1): number | null {
  const list = subscriptions.value
  if (list.length === 0) return null
  const idx = list.findIndex((s) => s.feed_id === selectedFeedId.value)
  if (idx === -1) return list[0].feed_id
  const next = idx + direction
  if (next < 0 || next >= list.length) return null
  return list[next].feed_id
}

export function applySubscriptionPatch(view: SubscriptionView): void {
  subscriptions.value = subscriptions.value.map((s) => (s.feed_id === view.feed_id ? view : s))
}

export function removeSubscription(feedId: number): void {
  subscriptions.value = subscriptions.value.filter((s) => s.feed_id !== feedId)
  if (selectedFeedId.value === feedId) {
    clearSelectedFeed()
  }
}

export function addSubscription(view: SubscriptionView): void {
  subscriptions.value = [...subscriptions.value, view]
}

export function adjustUnreadCount(feedId: number, delta: number): void {
  subscriptions.value = subscriptions.value.map((s) =>
    s.feed_id === feedId ? { ...s, unread_count: Math.max(0, s.unread_count + delta) } : s,
  )
}
