import { computed, effect, signal } from '@preact/signals'
import * as api from '../api/client'
import type { Folder, SubscriptionView } from '../api/types'
import { forgetFeed, hasVisitedFeed } from './navMemory'

export const subscriptions = signal<SubscriptionView[]>([])
export const folders = signal<Folder[]>([])
export const selectedFeedId = signal<number | null>(null)
export const loadingSubscriptions = signal(false)

/** Total unread count across all subscriptions -- drives both the sidebar
 * header badge (Sidebar.tsx) and the browser tab title (effect below), so
 * it's computed once here rather than duplicated in each consumer. */
export const totalUnreadCount = computed(() =>
  subscriptions.value.reduce((sum, s) => sum + s.unread_count, 0),
)

effect(() => {
  const count = totalUnreadCount.value
  document.title = count > 0 ? `(${count}) - feedla` : 'feedla'
})

/** Consecutive-failure threshold before a feed counts as "erroring" in the
 * UI (sidebar badge, フィード管理 の ⚠ エラーのみ view). A single blip (DNS
 * hiccup, one timed-out fetch) shouldn't surface here and tempt someone into
 * unsubscribing a feed that's actually fine -- only feeds failing 3 times in
 * a row are worth interrupting the user about. */
export const ERRORING_THRESHOLD = 3

export function isErroringFeed(s: SubscriptionView): boolean {
  return s.error_count >= ERRORING_THRESHOLD
}

const SIDEBAR_VIEW_MODE_KEY = 'feedla:sidebarViewMode'

function loadSidebarViewMode(): 'folder' | 'priority' {
  const stored = localStorage.getItem(SIDEBAR_VIEW_MODE_KEY)
  return stored === 'priority' ? 'priority' : 'folder'
}

/** How SubscriptionTree groups the sidebar: by folder ("カテゴリ") or by the
 * LDR-style ★ rating ("プライオリティ"). Same toggle drives both the desktop
 * two-pane layout and the mobile single-pane sidebar view -- there's no
 * separate mobile control, since the sidebar itself is just shown/hidden by
 * CSS on narrow viewports (see .has-selected-feed in global.css). Persisted
 * to localStorage so the choice survives a reload. */
export const sidebarViewMode = signal<'folder' | 'priority'>(
  loadSidebarViewMode(),
)

effect(() => {
  localStorage.setItem(SIDEBAR_VIEW_MODE_KEY, sidebarViewMode.value)
})

const COLLAPSED_GROUPS_KEY = 'feedla:collapsedGroups'

function loadCollapsedGroups(): Record<string, boolean> {
  const stored = localStorage.getItem(COLLAPSED_GROUPS_KEY)
  if (!stored) return {}
  try {
    const parsed = JSON.parse(stored)
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

/** Which SidebarGroup ids (e.g. "folder-3", "rating-5") are collapsed in
 * SubscriptionTree. Persisted to localStorage so the open/collapsed state
 * survives a reload, same as sidebarViewMode above. */
export const collapsedGroups = signal<Record<string, boolean>>(
  loadCollapsedGroups(),
)

effect(() => {
  localStorage.setItem(
    COLLAPSED_GROUPS_KEY,
    JSON.stringify(collapsedGroups.value),
  )
})

export function toggleGroupCollapsed(id: string): void {
  collapsedGroups.value = {
    ...collapsedGroups.value,
    [id]: !collapsedGroups.value[id],
  }
}

/** Expands the given sidebar group if it's currently collapsed -- a no-op
 * otherwise. Used by s/a keyboard navigation (see useKeyboardShortcuts) so
 * landing on a feed inside a folded folder/priority group reveals it instead
 * of leaving the selection invisible. */
export function ensureGroupExpanded(id: string): void {
  if (collapsedGroups.value[id]) {
    collapsedGroups.value = { ...collapsedGroups.value, [id]: false }
  }
}

/** A sidebar group ("Tech" folder, or the ★★★★★ priority level) selected as
 * a single merged reading target -- lets you read through every feed in the
 * group at once instead of picking feeds one by one. Mutually exclusive
 * with selectedFeedId (see selectFeed/clearSelectedFeed). */
export type GroupTarget =
  | { kind: 'folder'; folderId: number | null; label: string }
  | { kind: 'rating'; rating: number; label: string }
  | { kind: 'today'; label: string }

export const groupTarget = signal<GroupTarget | null>(null)

/** Whether the entry pane is showing search results instead of a feed/group
 * -- mutually exclusive with selectedFeedId/groupTarget (see selectFeed/
 * selectGroup/clearSelectedFeed), same as those. searchQuery is the last
 * submitted query, used both to re-fetch and to drive EntryItem's keyword
 * highlighting. */
export const searchMode = signal(false)
export const searchQuery = signal('')

/** Whether the entry pane is showing the feed management list (FeedManagerPane)
 * instead of a feed/group/search -- same mutual-exclusion pattern as
 * searchMode. feedManagerInitialOnlyErrors (state/ui.ts) is a one-shot flag
 * FeedManagerPane reads on mount, set by whichever caller opened it (see
 * state/actions.ts's openFeedManager). */
export const feedManagerMode = signal(false)

export function isSameGroupTarget(
  a: GroupTarget | null,
  b: GroupTarget,
): boolean {
  if (!a || a.kind !== b.kind) return false
  if (a.kind === 'folder' && b.kind === 'folder')
    return a.folderId === b.folderId
  if (a.kind === 'rating' && b.kind === 'rating') return a.rating === b.rating
  if (a.kind === 'today' && b.kind === 'today') return true
  return false
}

export function subscriptionsInFolder(
  folderId: number | null,
): SubscriptionView[] {
  return subscriptions.value.filter((s) => (s.folder_id ?? null) === folderId)
}

export function subscriptionsWithRating(rating: number): SubscriptionView[] {
  return subscriptions.value.filter((s) => s.rating === rating)
}

/** Unread count for the sidebar's pinned "Today" pseudo-group (past 24h,
 * across every feed regardless of rating) -- sourced from the server (see
 * loadSubscriptions), since it can't be derived from subscriptions'
 * all-time unread_count the way folder/rating groups can. */
export const todayUnreadCount = signal(0)

export function adjustTodayUnreadCount(delta: number): void {
  todayUnreadCount.value = Math.max(0, todayUnreadCount.value + delta)
}

export function groupUnreadCount(target: GroupTarget): number {
  if (target.kind === 'today') return todayUnreadCount.value
  const subs =
    target.kind === 'folder'
      ? subscriptionsInFolder(target.folderId)
      : subscriptionsWithRating(target.rating)
  return subs.reduce((sum, s) => sum + s.unread_count, 0)
}

const UNFILED_KEY = 0

export interface SidebarGroup {
  id: string
  name: string
  subs: SubscriptionView[]
  target: GroupTarget
}

/** The (has-unread, last-entry-timestamp) pair a feed is sorted by within
 * its カテゴリ/プライオリティ group -- see feedSortSnapshot below for why
 * this is captured rather than read live off `sub`. */
interface SortKey {
  hasUnread: boolean
  lastEntryAt: number
}

function liveSortKey(sub: SubscriptionView): SortKey {
  return {
    hasUnread: sub.unread_count > 0,
    lastEntryAt: sub.last_entry_at ?? 0,
  }
}

/** Freezes each feed's sort key at the moment subscriptions were last
 * (re)loaded from the server -- see captureSortSnapshot. Consulted instead
 * of the live `unread_count` so that reading entries (which decrements
 * unread_count in place via adjustUnreadCount) doesn't reshuffle the
 * sidebar mid-session; only an explicit reload (initial load, 'r' refresh,
 * add/remove subscription) does. */
const feedSortSnapshot = new Map<number, SortKey>()

function captureSortSnapshot(subs: SubscriptionView[]): void {
  feedSortSnapshot.clear()
  for (const sub of subs) feedSortSnapshot.set(sub.feed_id, liveSortKey(sub))
}

/** Test-only: clears the frozen sort keys, so a case that seeds them (via
 * addSubscription) can't leak its ordering into the next one. */
export function resetSortSnapshot(): void {
  feedSortSnapshot.clear()
}

function snapshotSortKey(sub: SubscriptionView): SortKey {
  return feedSortSnapshot.get(sub.feed_id) ?? liveSortKey(sub)
}

/** Orders feeds within a カテゴリ/プライオリティ group: unread feeds before
 * read-through ones, each half newest-entry-first, falling back to title
 * for feeds that tie on both (e.g. two freshly-subscribed feeds with the
 * same last-entry timestamp) -- see issue #33. */
function compareFeedsBySnapshot(
  a: SubscriptionView,
  b: SubscriptionView,
): number {
  const ka = snapshotSortKey(a)
  const kb = snapshotSortKey(b)
  if (ka.hasUnread !== kb.hasUnread) return ka.hasUnread ? -1 : 1
  if (ka.lastEntryAt !== kb.lastEntryAt) return kb.lastEntryAt - ka.lastEntryAt
  return (a.title || a.feed_url).localeCompare(b.title || b.feed_url)
}

/** Groups subscriptions by folder, in the order SubscriptionTree renders
 * them (folder sort_order/name, then "(未分類)" last), each group ordered
 * by compareFeedsBySnapshot -- the source of truth for both the sidebar's
 * カテゴリ view and s/a feed navigation, so the two never disagree about
 * what "next" means. */
export function buildGroupsByFolder(): SidebarGroup[] {
  const byFolder = new Map<number, SubscriptionView[]>()
  for (const sub of subscriptions.value) {
    const key = sub.folder_id ?? UNFILED_KEY
    const list = byFolder.get(key)
    if (list) {
      list.push(sub)
    } else {
      byFolder.set(key, [sub])
    }
  }

  const sortedFolders = [...folders.value].sort(
    (a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name),
  )

  const groups: SidebarGroup[] = []
  for (const f of sortedFolders) {
    const subs = byFolder.get(f.id)
    if (subs) {
      subs.sort(compareFeedsBySnapshot)
      groups.push({
        id: `folder-${f.id}`,
        name: f.name,
        subs,
        target: { kind: 'folder', folderId: f.id, label: f.name },
      })
    }
  }
  const unfiled = byFolder.get(UNFILED_KEY)
  if (unfiled) {
    unfiled.sort(compareFeedsBySnapshot)
    groups.push({
      id: `folder-${UNFILED_KEY}`,
      name: '(未分類)',
      subs: unfiled,
      target: { kind: 'folder', folderId: null, label: '(未分類)' },
    })
  }
  return groups
}

export function ratingLabel(rating: number): string {
  return rating === 0 ? '評価なし' : '★'.repeat(rating) + '☆'.repeat(5 - rating)
}

/** Groups by the LDR-style ★ rating (5 down to 0), highest priority first,
 * each group ordered by compareFeedsBySnapshot -- the source of truth for
 * both the sidebar's プライオリティ view and s/a feed navigation. */
export function buildGroupsByPriority(): SidebarGroup[] {
  const byRating = new Map<number, SubscriptionView[]>()
  for (const sub of subscriptions.value) {
    const list = byRating.get(sub.rating)
    if (list) {
      list.push(sub)
    } else {
      byRating.set(sub.rating, [sub])
    }
  }

  const groups: SidebarGroup[] = []
  for (let rating = 5; rating >= 0; rating--) {
    const subs = byRating.get(rating)
    if (!subs) continue
    subs.sort(compareFeedsBySnapshot)
    const label = ratingLabel(rating)
    groups.push({
      id: `rating-${rating}`,
      name: label,
      subs,
      target: { kind: 'rating', rating, label },
    })
  }
  return groups
}

/** The sidebar's pinned "Today" pseudo-group, unshifted above
 * buildGroupsByPriority's ★5..0 buckets when sidebarViewMode is 'priority'
 * (see SubscriptionTree). Unlike the folder/rating groups it has no subs of
 * its own -- its entries span every rated feed -- so `subs` stays empty and
 * SubscriptionTree special-cases it to skip the per-feed child rows/collapse
 * toggle. */
export const TODAY_GROUP: SidebarGroup = {
  id: 'today',
  name: 'Today',
  subs: [],
  target: { kind: 'today', label: 'Today' },
}

function currentSidebarGroups(): SidebarGroup[] {
  return sidebarViewMode.value === 'priority'
    ? buildGroupsByPriority()
    : buildGroupsByFolder()
}

/** Every feed_id in the order the sidebar currently renders them (respecting
 * sidebarViewMode), flattened across groups -- what s/a step through. */
export function displayedFeedOrder(): number[] {
  return currentSidebarGroups().flatMap((g) => g.subs.map((s) => s.feed_id))
}

/** The SidebarGroup id (e.g. "folder-3", "rating-5") containing the given
 * feed in the current sidebarViewMode, or null if the feed isn't in any
 * rendered group. Used to auto-expand a folded group when s/a keyboard
 * navigation lands on a feed inside it (see ensureGroupExpanded). */
export function groupIdForFeed(feedId: number): string | null {
  const group = currentSidebarGroups().find((g) =>
    g.subs.some((s) => s.feed_id === feedId),
  )
  return group ? group.id : null
}

export async function loadSubscriptions(): Promise<void> {
  loadingSubscriptions.value = true
  try {
    const [subsRes, foldersRes] = await Promise.all([
      api.listSubscriptions(),
      api.listFolders(),
    ])
    subscriptions.value = subsRes.subscriptions
    todayUnreadCount.value = subsRes.today_unread_count
    folders.value = foldersRes.folders
    captureSortSnapshot(subsRes.subscriptions)
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
  return (
    selectedFeedId.value !== null ||
    groupTarget.value !== null ||
    searchMode.value ||
    feedManagerMode.value
  )
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
  searchMode.value = false
  feedManagerMode.value = false
}

/** Deselects the current feed or group, returning to the subscription list.
 * On narrow (mobile) viewports this is what the entry pane's "戻る" back
 * button does -- on wide viewports the sidebar is visible regardless.
 * On mobile, this goes through history.back() (rather than only clearing
 * the signal) so it consumes the entry selectFeed/selectGroup pushed,
 * keeping the browser back stack in sync with the in-app navigation. The
 * signals are cleared synchronously here too (history.back()'s popstate
 * fires asynchronously) so a rapid double-tap of the back button sees
 * isInDetail() already false on the second call, instead of firing a
 * second history.back() that overshoots past feedla's base entry. */
export function clearSelectedFeed(): void {
  const goBack = isMobileViewport() && isInDetail()
  selectedFeedId.value = null
  groupTarget.value = null
  searchMode.value = false
  searchQuery.value = ''
  feedManagerMode.value = false
  if (goBack) {
    window.history.back()
  }
}

// One-shot override consumed by the next adjacentFeedId call -- see
// requestNavResetToHead.
let navResetPending = false

/** Makes the *next* s/a press land on the top of the current sidebar order
 * (same "nothing selected" head-of-list behavior adjacentFeedId already has)
 * without touching selectedFeedId/the entry pane -- called after 'r'
 * refetches+re-sorts the current feed, so pressing s/a right after a reload
 * resumes from the top of プライオリティ order (picking up anything that
 * just moved above the feed being read) instead of continuing on from where
 * the reader already was. */
export function requestNavResetToHead(): void {
  navResetPending = true
}

/** Next/previous feed from the current one, walking in the same order the
 * sidebar currently renders them (see displayedFeedOrder) -- what s/a and
 * Shift+J's "keep reading" flow (see useKeyboardShortcuts) step through,
 * and what prefetchNext (state/entries.ts) preloads, so all three always
 * agree on what "next" means. When nothing is selected yet (or
 * requestNavResetToHead fired since the last call), the scan starts from
 * the top of the list regardless of direction.
 *
 * Landing rule: a feed with unread entries left, so `s` never dumps the
 * reader on an empty feed. opts.includeVisited additionally accepts feeds
 * this tab has already opened (see state/navMemory.ts) -- `a` passes it so
 * that stepping back from a feed just finished with lands on that feed
 * rather than skipping over it to the one before, which is the whole
 * point of `a`. `s` deliberately does NOT pass it: stopping on read feeds
 * on the way forward would break burning through unreads with s.
 *
 * The head-scan path is exempt from includeVisited on purpose. It is the
 * "nothing selected" entry point shared by s and a alike, and the top of
 * the sidebar fills up with visited feeds as a session goes on, so
 * honoring it there would park `a` on the same long-finished feed every
 * time. Full spec: docs/keyboard-shortcuts.md. */
export function adjacentFeedId(
  direction: 1 | -1,
  opts?: { includeVisited?: boolean },
): number | null {
  const order = displayedFeedOrder()
  if (order.length === 0) return null
  const resetToHead = navResetPending
  navResetPending = false
  const idx =
    resetToHead || selectedFeedId.value === null
      ? -1
      : order.indexOf(selectedFeedId.value)
  const headScan = idx === -1
  const start = headScan ? 0 : idx + direction
  const step = headScan ? 1 : direction
  const includeVisited = !headScan && opts?.includeVisited === true
  for (let i = start; i >= 0 && i < order.length; i += step) {
    const feedId = order[i]
    const sub = subscriptions.value.find((s) => s.feed_id === feedId)
    if (!sub) continue
    if (sub.unread_count > 0) return feedId
    if (includeVisited && hasVisitedFeed(feedId)) return feedId
  }
  return null
}

export function applySubscriptionPatch(view: SubscriptionView): void {
  subscriptions.value = subscriptions.value.map((s) =>
    s.feed_id === view.feed_id ? view : s,
  )
}

export function removeSubscription(feedId: number): void {
  subscriptions.value = subscriptions.value.filter((s) => s.feed_id !== feedId)
  feedSortSnapshot.delete(feedId)
  forgetFeed(feedId)
  if (selectedFeedId.value === feedId) {
    clearSelectedFeed()
  }
}

/** Adds a newly-created subscription to the list, or replaces the existing
 * row if feed_id is already present -- re-subscribing to an already-known
 * feed (POST /api/v1/subscriptions upserts server-side) must update the one
 * row rather than appending a client-side duplicate on top of it. Also
 * seeds the new feed's sort snapshot (see feedSortSnapshot) so it gets a
 * stable sidebar position right away instead of drifting as it's read. */
export function addSubscription(view: SubscriptionView): void {
  const exists = subscriptions.value.some((s) => s.feed_id === view.feed_id)
  subscriptions.value = exists
    ? subscriptions.value.map((s) => (s.feed_id === view.feed_id ? view : s))
    : [...subscriptions.value, view]
  feedSortSnapshot.set(view.feed_id, liveSortKey(view))
}

export function adjustUnreadCount(feedId: number, delta: number): void {
  subscriptions.value = subscriptions.value.map((s) =>
    s.feed_id === feedId
      ? { ...s, unread_count: Math.max(0, s.unread_count + delta) }
      : s,
  )
}
