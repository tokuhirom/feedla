import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { Entry } from '../api/types'
import {
  adjacentFeedId,
  adjustUnreadCount,
  type GroupTarget,
  groupTarget,
  selectedFeedId,
  subscriptions,
} from './subscriptions'

export const entries = signal<Entry[]>([])
export const loadingEntries = signal(false)
export const focusedIndex = signal(0)

// One subscription's worth of entries, fetched ahead of time so pressing
// `s` feels instant. Only the immediately-next subscription is cached --
// see the Phase4 plan for why prefetching further ahead isn't worth it.
const prefetchCache = new Map<number, Entry[]>()

const IDLE_FLUSH_MS = 2000
const MAX_FLUSH_MS = 5000

const pendingReadIds = new Set<number>()
let idleTimer: ReturnType<typeof setTimeout> | null = null
let maxTimer: ReturnType<typeof setTimeout> | null = null

export async function loadEntries(feedId: number): Promise<void> {
  const cached = prefetchCache.get(feedId)
  if (cached) {
    entries.value = cached
    focusedIndex.value = 0
    prefetchCache.delete(feedId)
  } else {
    loadingEntries.value = true
  }

  try {
    const res = await api.listEntries(feedId, { unread: true, limit: 200 })
    if (selectedFeedId.value === feedId) {
      entries.value = res.entries
      focusedIndex.value = 0
    }
  } finally {
    loadingEntries.value = false
  }
}

/** Loads the merged unread list for a sidebar group (a folder or a
 * priority/★ level) -- see GroupTarget. Unlike loadEntries this never hits
 * the per-feed prefetch cache, since a group's entries span many feeds. */
export async function loadGroupEntries(target: GroupTarget): Promise<void> {
  loadingEntries.value = true
  try {
    const filter = target.kind === 'folder' ? { folderId: target.folderId } : { rating: target.rating }
    const res = await api.listGroupEntries(filter, { unread: true, limit: 200 })
    if (groupTarget.value === target) {
      entries.value = res.entries
      focusedIndex.value = 0
    }
  } finally {
    loadingEntries.value = false
  }
}

/** Moves the keyboard focus by one entry (j/k), marking the entry being
 * left behind as read when moving forward, and snapping it into view. */
export function moveFocus(direction: 1 | -1): void {
  const list = entries.value
  if (list.length === 0) return

  const current = focusedIndex.value
  if (direction === 1) {
    const leaving = list[current]
    if (leaving) markReadOptimistic(leaving.id)
  }

  const next = Math.min(Math.max(current + direction, 0), list.length - 1)
  focusedIndex.value = next

  const targetId = list[next]?.id
  if (targetId !== undefined) {
    requestAnimationFrame(() => {
      document.getElementById(`entry-${targetId}`)?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    })
  }
}

export async function prefetchNext(): Promise<void> {
  const nextId = adjacentFeedId(1)
  if (nextId === null || prefetchCache.has(nextId)) return
  const sub = subscriptions.value.find((s) => s.feed_id === nextId)
  if (!sub || sub.unread_count === 0) return

  try {
    const res = await api.listEntries(nextId, { unread: true, limit: 200 })
    prefetchCache.set(nextId, res.entries)
  } catch {
    // Best-effort: a failed prefetch just means loadEntries fetches for
    // real when the user gets there.
  }
}

/** Syncs Entry.pinned into the loaded entries list, so pin/unpin actions
 * taken from other surfaces (SearchOverlay, PinsOverlay) are reflected in
 * the entry pane's ★ indicator without a full reload. */
export function setEntryPinned(entryId: number, pinned: boolean): void {
  entries.value = entries.value.map((e) => (e.id === entryId ? { ...e, pinned } : e))
}

/** Marks an entry read locally (optimistic) and queues it for a debounced
 * bulk POST /api/v1/entries/read -- see the Phase4 plan for the two-tier
 * idle/max flush rationale. */
export function markReadOptimistic(entryId: number): void {
  const entry = entries.value.find((e) => e.id === entryId)
  if (!entry || entry.read_at != null) return

  entries.value = entries.value.map((e) =>
    e.id === entryId ? { ...e, read_at: Math.floor(Date.now() / 1000) } : e,
  )
  adjustUnreadCount(entry.feed_id, -1)

  pendingReadIds.add(entryId)
  if (idleTimer) clearTimeout(idleTimer)
  idleTimer = setTimeout(flushPendingReads, IDLE_FLUSH_MS)
  if (!maxTimer) {
    maxTimer = setTimeout(flushPendingReads, MAX_FLUSH_MS)
  }
}

export async function flushPendingReads(): Promise<void> {
  if (idleTimer) {
    clearTimeout(idleTimer)
    idleTimer = null
  }
  if (maxTimer) {
    clearTimeout(maxTimer)
    maxTimer = null
  }
  if (pendingReadIds.size === 0) return

  const ids = Array.from(pendingReadIds)
  pendingReadIds.clear()
  try {
    await api.markEntriesRead(ids)
  } catch {
    // Retry: put the ids back and let the next markReadOptimistic (or the
    // idle timer we just re-armed) flush them again.
    ids.forEach((id) => pendingReadIds.add(id))
    idleTimer = setTimeout(flushPendingReads, IDLE_FLUSH_MS)
  }
}

/** Best-effort flush for entries still queued when the user leaves --
 * fetch is unreliable during unload, so this uses sendBeacon instead. */
function flushOnUnload(): void {
  if (pendingReadIds.size === 0) return
  const ids = Array.from(pendingReadIds)
  pendingReadIds.clear()
  const blob = new Blob([JSON.stringify({ entry_ids: ids })], { type: 'application/json' })
  navigator.sendBeacon('/api/v1/entries/read', blob)
}

document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') flushOnUnload()
})
window.addEventListener('beforeunload', flushOnUnload)
