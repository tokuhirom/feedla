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

export function selectFeed(feedId: number): void {
  selectedFeedId.value = feedId
}

/** Deselects the current feed, returning to the subscription list. On
 * narrow (mobile) viewports this is what the entry pane's "戻る" back
 * button does -- on wide viewports the sidebar is visible regardless. */
export function clearSelectedFeed(): void {
  selectedFeedId.value = null
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
    selectedFeedId.value = null
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
