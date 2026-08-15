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

// .entry-pane is a single persistent DOM node never remounted between feed
// switches -- only its children (the .entry-item list) get swapped out. A
// stale leftover scrollTop from the previous feed can make
// useAutoMarkRead's IntersectionObserver see the newly loaded (unread)
// entries near the top as already "scrolled past" and mark them read on
// the spot. Explicitly reset it whenever a fresh entry list lands, rather
// than relying on incidental DOM-diffing behavior to do it.
function resetEntryPaneScroll(): void {
  document.querySelector('.entry-pane')?.scrollTo(0, 0)
}

// Guards against two overlapping loadEntries/loadGroupEntries calls (e.g.
// AddSubscriptionDialog's post-subscribe load racing a user click on the
// same feed) landing out of order: without this, an older in-flight
// response can resolve after a newer one and clobber entries.value with
// stale data. Only the call that was most recently started is allowed to
// apply its response.
let loadToken = 0

/** A freshly-fetched list reflects the server's view as of whenever the
 * request was *sent*, which can be older than a read the user has since
 * marked optimistically (see markReadOptimistic) -- e.g. AddSubscriptionDialog
 * fires its own loadEntries right before a user click re-triggers one via
 * selectAndLoadFeed, and the second request's snapshot predates a 'j' press
 * that happens before it resolves. pendingReadIds is exactly "reads the
 * server doesn't know about yet", so re-apply read_at for any id still in
 * there instead of letting the fetch silently revert it to unread. */
function withPendingReadsApplied(list: Entry[]): Entry[] {
  if (pendingReadIds.size === 0) return list
  const now = Math.floor(Date.now() / 1000)
  return list.map((e) => (pendingReadIds.has(e.id) && e.read_at == null ? { ...e, read_at: now } : e))
}

export async function loadEntries(feedId: number): Promise<void> {
  const token = ++loadToken
  const cached = prefetchCache.get(feedId)
  if (cached) {
    entries.value = withPendingReadsApplied(cached)
    focusedIndex.value = 0
    resetEntryPaneScroll()
    prefetchCache.delete(feedId)
  } else {
    loadingEntries.value = true
  }

  try {
    const res = await api.listEntries(feedId, { unread: true, limit: 200 })
    if (token === loadToken && selectedFeedId.value === feedId) {
      entries.value = withPendingReadsApplied(res.entries)
      focusedIndex.value = 0
      resetEntryPaneScroll()
    }
  } finally {
    if (token === loadToken) loadingEntries.value = false
  }
}

/** Loads the merged unread list for a sidebar group (a folder or a
 * priority/★ level) -- see GroupTarget. Unlike loadEntries this never hits
 * the per-feed prefetch cache, since a group's entries span many feeds. */
export async function loadGroupEntries(target: GroupTarget): Promise<void> {
  const token = ++loadToken
  loadingEntries.value = true
  try {
    const filter = target.kind === 'folder' ? { folderId: target.folderId } : { rating: target.rating }
    const res = await api.listGroupEntries(filter, { unread: true, limit: 200 })
    if (token === loadToken && groupTarget.value === target) {
      entries.value = withPendingReadsApplied(res.entries)
      focusedIndex.value = 0
      resetEntryPaneScroll()
    }
  } finally {
    if (token === loadToken) loadingEntries.value = false
  }
}

/** The first entry not yet scrolled entirely behind the sticky
 * .entry-header -- i.e. whatever's currently at the top of the reading
 * position. */
export function topOfViewIndex(list: Entry[], container: HTMLElement, viewTop: number): number {
  for (const item of container.querySelectorAll<HTMLElement>('.entry-item[data-entry-id]')) {
    if (item.getBoundingClientRect().bottom > viewTop) {
      const idx = list.findIndex((e) => e.id === Number(item.dataset.entryId))
      if (idx !== -1) return idx
    }
  }
  return list.length - 1
}

/** j/k's anchor point: normally just focusedIndex.value (the entry
 * moveFocus itself last placed at the top), so short entry lists that never
 * need scrolling behave exactly as before. But if the reader has scrolled
 * the focused entry away with the mouse wheel since the last j/k press
 * (useAutoMarkRead's own scroll-driven marking has the same effect),
 * focusedIndex.value no longer matches what's on screen -- in that case
 * resync to whichever entry is now actually at the top, so j/k continue
 * from the reader's real position instead of a stale one. See issue #37. */
function currentScrollIndex(list: Entry[]): number {
  const idx = focusedIndex.value
  const container = document.querySelector('.entry-pane')
  if (!(container instanceof HTMLElement)) return idx

  const header = container.querySelector('.entry-header')
  const headerHeight = header instanceof HTMLElement ? header.getBoundingClientRect().height : 0
  const viewTop = container.getBoundingClientRect().top + headerHeight

  const focusedEntry = list[idx]
  const focusedEl = focusedEntry && document.getElementById(`entry-${focusedEntry.id}`)
  if (focusedEl && focusedEl.getBoundingClientRect().bottom > viewTop) {
    // Still at (or below) the reading position -- untouched since the last
    // j/k press, including the common case where nothing on the page
    // scrolls at all.
    return idx
  }
  return topOfViewIndex(list, container, viewTop)
}

/** Whether the reader's current scroll position is already on the last
 * entry -- what Shift+J (see useKeyboardShortcuts) uses to decide between
 * "act like j" and "move to the next feed", so that decision also follows
 * scroll position rather than a possibly-stale focusedIndex. */
export function isAtLastVisibleEntry(): boolean {
  const list = entries.value
  return list.length === 0 || currentScrollIndex(list) >= list.length - 1
}

// Set while moveFocus's own smooth-scroll animation is in flight, so
// syncFocusToScroll (driven by touch/wheel scrolling, see
// useScrollFocusSync) doesn't fight it -- without this, the ring would
// flicker back to the entry being left before landing back on `next` once
// the animation catches up.
let programmaticScroll = false

/** Moves the keyboard focus by one entry (j/k), marking the entry being
 * left behind as read when moving forward, and snapping it into view. */
export function moveFocus(direction: 1 | -1): void {
  const list = entries.value
  if (list.length === 0) return

  const current = currentScrollIndex(list)
  if (direction === 1) {
    const leaving = list[current]
    if (leaving) markReadOptimistic(leaving.id)
  }

  const next = Math.min(Math.max(current + direction, 0), list.length - 1)
  focusedIndex.value = next

  const targetId = list[next]?.id
  if (targetId !== undefined) {
    requestAnimationFrame(() => {
      const target = document.getElementById(`entry-${targetId}`)
      const container = target?.closest('.entry-pane')
      if (!(target instanceof HTMLElement) || !(container instanceof HTMLElement)) return

      // scrollIntoView({ block: 'start' }) would align the entry's top edge
      // with the container's top edge, which is exactly where the sticky
      // .entry-header sits -- hiding the entry title behind it. Offset by
      // the header's live height instead of a hardcoded constant, since it
      // wraps to multiple lines on narrow viewports.
      const header = container.querySelector('.entry-header')
      const headerHeight = header instanceof HTMLElement ? header.getBoundingClientRect().height : 0
      const targetTop = target.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop

      programmaticScroll = true
      container.addEventListener('scrollend', () => { programmaticScroll = false }, { once: true })
      // Fallback for browsers without 'scrollend' (Safari < 16.4), and for
      // the case where targetTop === current scrollTop so no scroll (and
      // thus no 'scrollend') ever fires.
      setTimeout(() => { programmaticScroll = false }, 500)

      container.scrollTo({ top: targetTop - headerHeight, behavior: 'smooth' })
    })
  }
}

/** Keeps focusedIndex following the reading position as the reader
 * scrolls -- called from useScrollFocusSync. This is the only way the
 * focus ring moves on touch devices, which have no j/k to drive
 * moveFocus. */
export function syncFocusToScroll(): void {
  if (programmaticScroll) return

  const list = entries.value
  if (list.length === 0) return

  const container = document.querySelector('.entry-pane')
  if (!(container instanceof HTMLElement)) return

  const header = container.querySelector('.entry-header')
  const headerHeight = header instanceof HTMLElement ? header.getBoundingClientRect().height : 0
  const viewTop = container.getBoundingClientRect().top + headerHeight

  const idx = topOfViewIndex(list, container, viewTop)
  if (idx !== focusedIndex.value) focusedIndex.value = idx
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
