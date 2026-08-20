import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { Entry } from '../api/types'
import { recallFocusedEntry, rememberFocusedEntry } from './navMemory'
import {
  adjacentFeedId,
  adjustTodayUnreadCount,
  adjustUnreadCount,
  type GroupTarget,
  groupTarget,
  searchMode,
  searchQuery,
  selectedFeedId,
  subscriptions,
} from './subscriptions'

export const entries = signal<Entry[]>([])
export const loadingEntries = signal(false)
export const focusedIndex = signal(0)
// True when entries.value is a fallback list of recent *read* entries shown
// because a single feed (loadEntries) had zero unread ones -- lets EntryPane
// tell the reader "these are read" instead of implying a fresh unread list.
// See loadEntries for why this exists; always reset to false by any loader
// that isn't that fallback path, so it doesn't linger across navigation.
export const entriesShowingReadFallback = signal(false)

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
  return list.map((e) =>
    pendingReadIds.has(e.id) && e.read_at == null ? { ...e, read_at: now } : e,
  )
}

/** Where the focus ring should land in a freshly loaded list for `feedId`:
 * the entry the reader was last on in that feed (see state/navMemory.ts) if
 * it's still in the list, otherwise the top. This is what makes `a` land on
 * the article the reader walked away from rather than the feed's newest
 * one. See docs/keyboard-shortcuts.md.
 *
 * Bails out when anything above the remembered entry is still unread:
 * scrolling down to it drags everything above past the pane's top edge,
 * which useAutoMarkRead treats as read. On the fully-read list this restore
 * normally runs against that's a no-op, but a feed that picked up newer
 * unread entries since the reader left must not have them silently consumed
 * by coming back. */
function restoredFocusIndex(feedId: number, list: Entry[]): number {
  const entryId = recallFocusedEntry(feedId)
  if (entryId === undefined) return 0
  const idx = list.findIndex((e) => e.id === entryId)
  if (idx <= 0) return 0
  if (list.slice(0, idx).some((e) => e.read_at == null)) return 0
  return idx
}

function applyLoadedEntries(
  feedId: number,
  list: Entry[],
  readFallback: boolean,
): void {
  entries.value = withPendingReadsApplied(list)
  entriesShowingReadFallback.value = readFallback
  const idx = restoredFocusIndex(feedId, entries.value)
  focusedIndex.value = idx
  // Always clear the previous feed's scrollTop first (see
  // resetEntryPaneScroll); the restore below then scrolls down from a known
  // position once the new list has rendered.
  resetEntryPaneScroll()
  const target = entries.value[idx]
  if (idx > 0 && target) scrollEntryIntoView(target.id, 'auto')
}

export async function loadEntries(feedId: number): Promise<void> {
  const token = ++loadToken
  const cached = prefetchCache.get(feedId)
  if (cached) {
    applyLoadedEntries(feedId, cached, false)
    prefetchCache.delete(feedId)
  } else {
    loadingEntries.value = true
  }

  try {
    const res = await api.listEntries(feedId, { unread: true, limit: 200 })
    if (token === loadToken && selectedFeedId.value === feedId) {
      if (res.entries.length > 0) {
        applyLoadedEntries(feedId, res.entries, false)
      } else {
        // No unread entries -- the reader has no way to tell a feed is
        // sitting empty from one that's simply been fully read, so fall
        // back to its most recent (read) entries instead of just "no
        // unread". opts.unread omitted fetches all entries regardless of
        // read state. The limit matches the unread query's rather than
        // being a token handful, because this is the list `a` restores a
        // remembered reading position into -- see restoredFocusIndex, and
        // docs/keyboard-shortcuts.md for why `a` depends on it.
        const fallback = await api.listEntries(feedId, { limit: 200 })
        if (token === loadToken && selectedFeedId.value === feedId) {
          applyLoadedEntries(feedId, fallback.entries, true)
        }
      }
    }
  } finally {
    if (token === loadToken) loadingEntries.value = false
  }
}

/** Reorders a flat (published_at DESC) entry list into rating buckets --
 * 5..0 including unrated, each bucket's relative order preserved (already
 * published_at DESC from the server) -- for the Today group. Physically
 * reordering entries.value itself (rather than only reordering at render
 * time) keeps moveFocus/topOfViewIndex's DOM-order-matches-array-order
 * assumption intact. */
function groupEntriesByRating(list: Entry[]): Entry[] {
  const byRating = new Map<number, Entry[]>()
  for (const e of list) {
    const rating =
      subscriptions.value.find((s) => s.feed_id === e.feed_id)?.rating ?? 0
    const bucket = byRating.get(rating)
    if (bucket) bucket.push(e)
    else byRating.set(rating, [e])
  }
  const out: Entry[] = []
  for (let rating = 5; rating >= 0; rating--) {
    const bucket = byRating.get(rating)
    if (bucket) out.push(...bucket)
  }
  return out
}

/** Loads the merged unread list for a sidebar group (a folder, a
 * priority/★ level, or the Today pseudo-group) -- see GroupTarget. Unlike
 * loadEntries this never hits the per-feed prefetch cache, since a group's
 * entries span many feeds. Today entries are additionally re-bucketed by
 * rating (see groupEntriesByRating) so EntryPane can render rating section
 * headings; Today fetches up to 500 in one shot rather than following
 * next_cursor, sidestepping the "rating buckets split across pages"
 * problem for personal-scale unread volumes. */
export async function loadGroupEntries(target: GroupTarget): Promise<void> {
  const token = ++loadToken
  loadingEntries.value = true
  try {
    const res =
      target.kind === 'today'
        ? await api.listTodayEntries({ limit: 500 })
        : await api.listGroupEntries(
            target.kind === 'folder'
              ? { folderId: target.folderId }
              : { rating: target.rating },
            { unread: true, limit: 200 },
          )
    if (token === loadToken && groupTarget.value === target) {
      const ordered =
        target.kind === 'today'
          ? groupEntriesByRating(res.entries)
          : res.entries
      entries.value = withPendingReadsApplied(ordered)
      entriesShowingReadFallback.value = false
      focusedIndex.value = 0
      resetEntryPaneScroll()
    }
  } finally {
    if (token === loadToken) loadingEntries.value = false
  }
}

/** Loads search results into the normal entries pipeline (same read/pin/
 * scroll behavior as a feed or group) -- see state/actions.ts's runSearch,
 * which is what actually sets searchMode/searchQuery before calling this.
 * Guards on searchQuery.value === query rather than just the loadToken so a
 * response for an abandoned query can't clobber a newer one that happened
 * to resolve first. */
export async function loadSearchEntries(query: string): Promise<void> {
  const token = ++loadToken
  loadingEntries.value = true
  try {
    const res = await api.searchEntries(query, { limit: 200 })
    if (
      token === loadToken &&
      searchMode.value &&
      searchQuery.value === query
    ) {
      entries.value = withPendingReadsApplied(res.entries)
      entriesShowingReadFallback.value = false
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
export function topOfViewIndex(
  list: Entry[],
  container: HTMLElement,
  viewTop: number,
): number {
  for (const item of container.querySelectorAll<HTMLElement>(
    '.entry-item[data-entry-id]',
  )) {
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
  const headerHeight =
    header instanceof HTMLElement ? header.getBoundingClientRect().height : 0
  const viewTop = container.getBoundingClientRect().top + headerHeight

  const focusedEntry = list[idx]
  const focusedEl =
    focusedEntry && document.getElementById(`entry-${focusedEntry.id}`)
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

// Bumped by every moveFocus so the settle correction of a superseded j/k
// press (its scrollend listener and 500ms fallback timer both stay armed)
// can recognize it's stale and bail out instead of yanking the pane back
// toward its old target mid-way through the newer press's scroll.
let scrollSeq = 0

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
  if (targetId !== undefined) scrollEntryIntoView(targetId)
}

/** Scrolls the entry pane so the entry with `targetId` comes to rest just
 * below the sticky .entry-header, correcting for content-visibility
 * placeholder heights until the position stops moving. Shared by j/k
 * (moveFocus) and by loadEntries restoring a remembered reading position
 * (see restoredFocusIndex). */
function scrollEntryIntoView(
  targetId: number,
  behavior: ScrollBehavior = 'smooth',
): void {
  requestAnimationFrame(() => {
    const target = document.getElementById(`entry-${targetId}`)
    const container = target?.closest('.entry-pane')
    if (!(target instanceof HTMLElement) || !(container instanceof HTMLElement))
      return

    // scrollIntoView({ block: 'start' }) would align the entry's top edge
    // with the container's top edge, which is exactly where the sticky
    // .entry-header sits -- hiding the entry title behind it. Offset by
    // the header's live height instead of a hardcoded constant, since it
    // wraps to multiple lines on narrow viewports.
    const header = container.querySelector('.entry-header')
    const headerHeight =
      header instanceof HTMLElement ? header.getBoundingClientRect().height : 0
    const targetTop =
      target.getBoundingClientRect().top -
      container.getBoundingClientRect().top +
      container.scrollTop

    // .entry-item has content-visibility: auto, so an off-screen entry
    // (including ones the scroll is about to pass over) is laid out with
    // a placeholder size until it nears the viewport -- targetTop above
    // is computed from those placeholder heights, not the real ones. For
    // an image-heavy post that placeholder can be well short of the
    // actual height, so the smooth scroll lands short of the entry's true
    // top. Once the scroll settles the browser has since rendered
    // everything it passed over for real, so re-measure and snap to the
    // now-accurate position.
    const seq = ++scrollSeq
    // Each scrollTop correction below can itself reveal more
    // placeholder-sized entries whose real layout shifts the target
    // again, so a single correction routinely lands short -- re-measure
    // every frame until the position stops moving. Capped so a
    // pathological layout (e.g. an image resizing under max-height on
    // every load) can't loop forever.
    let settleTriesLeft = 20
    const settle = (): void => {
      if (seq !== scrollSeq) return // superseded by a newer j/k press
      // How far the target's top currently sits from its desired resting
      // position (just below the sticky header) -- a relative delta, NOT
      // an absolute scrollTop, since it's applied with `+=` below.
      const drift =
        target.getBoundingClientRect().top -
        container.getBoundingClientRect().top -
        headerHeight
      if (Math.abs(drift) > 1 && settleTriesLeft-- > 0) {
        container.scrollTop += drift
        requestAnimationFrame(settle)
        return
      }
      programmaticScroll = false
    }

    programmaticScroll = true
    container.addEventListener('scrollend', settle, { once: true })
    // Fallback for browsers without 'scrollend' (Safari < 16.4), and for
    // the case where targetTop === current scrollTop so no scroll (and
    // thus no 'scrollend') ever fires.
    setTimeout(settle, 500)

    container.scrollTo({ top: targetTop - headerHeight, behavior })
  })
}

// Mirrors global.css's mobile breakpoint (single-pane layout, no
// keyboard-driven j/k in practice). Keep in sync with that value.
const MOBILE_BREAKPOINT_QUERY = '(max-width: 700px)'

/** Keeps focusedIndex following the reading position as the reader
 * scrolls -- called from useScrollFocusSync. This is the only way the
 * focus ring moves on touch devices, which have no j/k to drive
 * moveFocus. Scoped to the mobile layout: on desktop, moveFocus's own
 * currentScrollIndex() resync already covers wheel-scrolled drift on the
 * next j/k press (see issue #37), and continuously chasing every wheel
 * tick here would fight that -- the ring is meant to stay put between
 * keypresses there. */
export function syncFocusToScroll(): void {
  if (programmaticScroll) return
  if (!window.matchMedia(MOBILE_BREAKPOINT_QUERY).matches) return

  const list = entries.value
  if (list.length === 0) return

  const container = document.querySelector('.entry-pane')
  if (!(container instanceof HTMLElement)) return

  const header = container.querySelector('.entry-header')
  const headerHeight =
    header instanceof HTMLElement ? header.getBoundingClientRect().height : 0
  const viewTop = container.getBoundingClientRect().top + headerHeight

  const idx = topOfViewIndex(list, container, viewTop)
  if (idx !== focusedIndex.value) focusedIndex.value = idx
}

export async function prefetchNext(): Promise<void> {
  // adjacentFeedId(1) already only returns feeds with unread entries left.
  const nextId = adjacentFeedId(1)
  if (nextId === null || prefetchCache.has(nextId)) return

  try {
    const res = await api.listEntries(nextId, { unread: true, limit: 200 })
    prefetchCache.set(nextId, res.entries)
  } catch {
    // Best-effort: a failed prefetch just means loadEntries fetches for
    // real when the user gets there.
  }
}

/** Syncs Entry.pinned into the loaded entries list, so pin/unpin actions
 * taken from PinsOverlay are reflected in the entry pane's ★ indicator
 * without a full reload. */
export function setEntryPinned(entryId: number, pinned: boolean): void {
  entries.value = entries.value.map((e) =>
    e.id === entryId ? { ...e, pinned } : e,
  )
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
  if (entry.published_at >= Math.floor(Date.now() / 1000) - 86400) {
    adjustTodayUnreadCount(-1)
  }

  pendingReadIds.add(entryId)
  if (idleTimer) clearTimeout(idleTimer)
  idleTimer = setTimeout(flushPendingReads, IDLE_FLUSH_MS)
  if (!maxTimer) {
    maxTimer = setTimeout(flushPendingReads, MAX_FLUSH_MS)
  }
}

/** Marks every currently-rendered entry that fits entirely within the pane's
 * visible viewport (nothing left to scroll past) as read. useAutoMarkRead's
 * IntersectionObserver/scroll paths only catch an entry once the reader has
 * actually scrolled it out of view, so a feed/group that's short enough to
 * need no scrolling at all -- or a switch away before any scroll happens --
 * would otherwise leave entries the reader plainly saw stuck as unread.
 * Called right before navigating to a different feed/group; see
 * selectAndLoadFeed/selectGroup in state/actions.ts. */
/** Records where the reader is in the currently selected feed, so coming
 * back to it later (`a`, or just re-selecting it) can resume there instead
 * of the top of the list. Called on the way out of a feed by
 * selectAndLoadFeed.
 *
 * Deliberately reads focusedIndex rather than resolving the true on-screen
 * position the way currentScrollIndex does: useScrollFocusSync already
 * keeps focusedIndex following the reading position, the worst case is
 * landing a single entry off, and staying free of getBoundingClientRect
 * keeps this verifiable in jsdom unit tests rather than only in e2e. */
export function rememberFocusedEntryForCurrentFeed(): void {
  const feedId = selectedFeedId.value
  if (feedId === null) return
  const entry = entries.value[focusedIndex.value]
  if (entry) rememberFocusedEntry(feedId, entry.id)
}

export function markVisibleEntriesRead(): void {
  const container = document.querySelector('.entry-pane')
  if (!(container instanceof HTMLElement)) return

  const header = container.querySelector('.entry-header')
  const headerHeight =
    header instanceof HTMLElement ? header.getBoundingClientRect().height : 0
  const paneRect = container.getBoundingClientRect()
  const viewTop = paneRect.top + headerHeight

  for (const el of container.querySelectorAll<HTMLElement>(
    '.entry-item[data-entry-id]',
  )) {
    const rect = el.getBoundingClientRect()
    if (rect.top < viewTop || rect.bottom > paneRect.bottom) continue
    const id = Number(el.dataset.entryId)
    if (Number.isFinite(id)) markReadOptimistic(id)
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
    ids.forEach((id) => {
      pendingReadIds.add(id)
    })
    idleTimer = setTimeout(flushPendingReads, IDLE_FLUSH_MS)
  }
}

/** Best-effort flush for entries still queued when the user leaves --
 * fetch is unreliable during unload, so this uses sendBeacon instead. */
function flushOnUnload(): void {
  if (pendingReadIds.size === 0) return
  const ids = Array.from(pendingReadIds)
  pendingReadIds.clear()
  const blob = new Blob([JSON.stringify({ entry_ids: ids })], {
    type: 'application/json',
  })
  navigator.sendBeacon('/api/v1/entries/read', blob)
}

document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') flushOnUnload()
})
window.addEventListener('beforeunload', flushOnUnload)
