// Mirrors the app's screen state (selected feed/group/search/フィード管理
// + which overlays are open) into the browser URL, and restores it back on
// load/reload/back-forward -- without this, reloading always dropped back
// to the top-level view. Kept as a single module so every signal <-> URL
// edge lives in one place instead of scattered across each screen
// transition (state/actions.ts's selectFeed/selectGroup/openSearch/
// runSearch/openFeedManager, and every overlay's own openOpen.value = true)
// -- those keep writing to their signals exactly as before; startUrlSync
// below is the only thing that ever writes to the URL.
import { batch, effect, type Signal } from '@preact/signals'
import { SUBSCRIPTION_KINDS, type SubscriptionKind } from '../api/types'
import {
  entries,
  loadEntries,
  loadGroupEntries,
  loadSearchEntries,
} from './entries'
import {
  type FeedManagerKindFilter,
  feedManagerErrorNeedle,
  feedManagerKindFilter,
  feedManagerMinErrorCount,
  feedManagerOnlyErrors,
  feedManagerQuery,
  feedManagerUrlNeedle,
} from './feedManager'
import { pinsOpen } from './pins'
import { statsOpen } from './stats'
import {
  feedManagerMode,
  folders,
  type GroupTarget,
  groupTarget,
  ratingLabel,
  searchMode,
  searchQuery,
  selectedFeedId,
} from './subscriptions'
import { helpOpen } from './ui'

// Only overlays that are read-only *views* (nothing to lose by reopening
// them) are URL-synced. Action/editing modals -- AddSubscriptionDialog,
// FeedDetailOverlay, AdminOverlay, IgnoreWordsOverlay -- deliberately stay
// out: each one calls a screen-transition function (selectFeed et al, which
// pushMobileDetailNav below reacts to) from *inside* an async handler,
// sometimes closing the modal itself only after that transition already
// landed. Syncing them would bake that in-between "modal still open" state
// into whatever history entry pushMobileDetailNav pushes/replaces at that
// exact instant, so reopening it later (back/forward, or a reload) would
// resurrect a modal that was actually already on its way to closing -- see
// e.g. AddSubscriptionDialog's onSubscribed (selectFeed) running before its
// own close(). The underlying rule, if a future overlay needs adding here:
// close the overlay's own signal BEFORE calling a screen-transition
// function from inside it, never after -- a "select feed then close
// overlay" handler (e.g. a hypothetical PinsOverlay row that both jumps to
// the entry's feed and closes the pin list) reintroduces this exact bug the
// moment that overlay gets added to OVERLAY_SIGNALS below.
export type OverlayId = 'help' | 'pins' | 'stats'

const OVERLAY_SIGNALS: Record<OverlayId, Signal<boolean>> = {
  help: helpOpen,
  pins: pinsOpen,
  stats: statsOpen,
}

const OVERLAY_IDS = Object.keys(OVERLAY_SIGNALS) as OverlayId[]

export interface FeedManagerFilters {
  kindFilter: FeedManagerKindFilter
  query: string
  onlyErrors: boolean
  minErrorCount: string
  urlNeedle: string
  errorNeedle: string
}

export type MainView =
  | { kind: 'home' }
  | { kind: 'feed'; feedId: number }
  | { kind: 'group'; target: GroupTarget }
  | { kind: 'search'; query: string }
  | { kind: 'manage'; filters: FeedManagerFilters }

export interface RouteState {
  view: MainView
  overlays: OverlayId[]
}

function folderLabel(folderId: number | null): string {
  if (folderId === null) return '(未分類)'
  return folders.value.find((f) => f.id === folderId)?.name ?? '(未分類)'
}

function encodeMainView(view: MainView): {
  path: string
  params: URLSearchParams
} {
  const params = new URLSearchParams()
  switch (view.kind) {
    case 'home':
      return { path: '/', params }
    case 'feed':
      return { path: `/feed/${view.feedId}`, params }
    case 'group': {
      const t = view.target
      if (t.kind === 'folder') {
        return {
          path: `/group/folder/${t.folderId === null ? 'unfiled' : t.folderId}`,
          params,
        }
      }
      if (t.kind === 'rating') {
        return { path: `/group/rating/${t.rating}`, params }
      }
      return { path: '/group/today', params }
    }
    case 'search':
      if (view.query) params.set('q', view.query)
      return { path: '/search', params }
    case 'manage': {
      const f = view.filters
      if (f.kindFilter !== 'all') params.set('kind', f.kindFilter)
      if (f.query) params.set('q', f.query)
      if (f.onlyErrors) params.set('err', '1')
      if (f.minErrorCount) params.set('minErr', f.minErrorCount)
      if (f.urlNeedle) params.set('url', f.urlNeedle)
      if (f.errorNeedle) params.set('errMsg', f.errorNeedle)
      return { path: '/manage', params }
    }
  }
}

export function encodeRoute(route: RouteState): string {
  const { path, params } = encodeMainView(route.view)
  if (route.overlays.length > 0) {
    params.set('ov', route.overlays.join(','))
  }
  const qs = params.toString()
  return qs ? `${path}?${qs}` : path
}

function decodeOverlays(params: URLSearchParams): OverlayId[] {
  const raw = params.get('ov')
  if (!raw) return []
  return raw
    .split(',')
    .filter((id): id is OverlayId => OVERLAY_IDS.includes(id as OverlayId))
}

function isKindFilter(v: string | null): v is SubscriptionKind {
  return v !== null && (SUBSCRIPTION_KINDS as string[]).includes(v)
}

export function decodeRoute(loc: {
  pathname: string
  search: string
}): RouteState {
  const params = new URLSearchParams(loc.search)
  const overlays = decodeOverlays(params)
  const path = loc.pathname

  const feedMatch = /^\/feed\/(\d+)$/.exec(path)
  if (feedMatch) {
    return { view: { kind: 'feed', feedId: Number(feedMatch[1]) }, overlays }
  }

  const folderMatch = /^\/group\/folder\/(unfiled|\d+)$/.exec(path)
  if (folderMatch) {
    const folderId =
      folderMatch[1] === 'unfiled' ? null : Number(folderMatch[1])
    return {
      view: {
        kind: 'group',
        target: { kind: 'folder', folderId, label: folderLabel(folderId) },
      },
      overlays,
    }
  }

  const ratingMatch = /^\/group\/rating\/([0-5])$/.exec(path)
  if (ratingMatch) {
    const rating = Number(ratingMatch[1])
    return {
      view: {
        kind: 'group',
        target: { kind: 'rating', rating, label: ratingLabel(rating) },
      },
      overlays,
    }
  }

  if (path === '/group/today') {
    return {
      view: { kind: 'group', target: { kind: 'today', label: 'Today' } },
      overlays,
    }
  }

  if (path === '/search') {
    return { view: { kind: 'search', query: params.get('q') ?? '' }, overlays }
  }

  if (path === '/manage') {
    const kindParam = params.get('kind')
    return {
      view: {
        kind: 'manage',
        filters: {
          kindFilter: isKindFilter(kindParam) ? kindParam : 'all',
          query: params.get('q') ?? '',
          onlyErrors: params.get('err') === '1',
          minErrorCount: params.get('minErr') ?? '',
          urlNeedle: params.get('url') ?? '',
          errorNeedle: params.get('errMsg') ?? '',
        },
      },
      overlays,
    }
  }

  return { view: { kind: 'home' }, overlays }
}

/** Reads the current screen state straight off the signals -- what
 * startUrlSync's effect encodes into the URL on every relevant change. */
export function currentRouteFromSignals(): RouteState {
  const overlays = OVERLAY_IDS.filter((id) => OVERLAY_SIGNALS[id].value)

  if (selectedFeedId.value !== null) {
    return { view: { kind: 'feed', feedId: selectedFeedId.value }, overlays }
  }
  if (groupTarget.value !== null) {
    return { view: { kind: 'group', target: groupTarget.value }, overlays }
  }
  if (searchMode.value) {
    return { view: { kind: 'search', query: searchQuery.value }, overlays }
  }
  if (feedManagerMode.value) {
    return {
      view: {
        kind: 'manage',
        filters: {
          kindFilter: feedManagerKindFilter.value,
          query: feedManagerQuery.value,
          onlyErrors: feedManagerOnlyErrors.value,
          minErrorCount: feedManagerMinErrorCount.value,
          urlNeedle: feedManagerUrlNeedle.value,
          errorNeedle: feedManagerErrorNeedle.value,
        },
      },
      overlays,
    }
  }
  return { view: { kind: 'home' }, overlays }
}

/** Writes a decoded route back onto the signals -- the inverse of
 * currentRouteFromSignals. Only touches signals, never fetches data (see
 * loadDataForRoute for that half), so it's safe to call synchronously from
 * both the initial mount and every popstate. */
export function applyRouteToSignals(route: RouteState): void {
  batch(() => {
    selectedFeedId.value = route.view.kind === 'feed' ? route.view.feedId : null
    groupTarget.value = route.view.kind === 'group' ? route.view.target : null
    searchMode.value = route.view.kind === 'search'
    searchQuery.value = route.view.kind === 'search' ? route.view.query : ''
    feedManagerMode.value = route.view.kind === 'manage'
    if (route.view.kind === 'manage') {
      const f = route.view.filters
      feedManagerKindFilter.value = f.kindFilter
      feedManagerQuery.value = f.query
      feedManagerOnlyErrors.value = f.onlyErrors
      feedManagerMinErrorCount.value = f.minErrorCount
      feedManagerUrlNeedle.value = f.urlNeedle
      feedManagerErrorNeedle.value = f.errorNeedle
    }
    for (const id of OVERLAY_IDS) {
      OVERLAY_SIGNALS[id].value = route.overlays.includes(id)
    }
  })
}

/** The async half of restoring a route: fetches whatever entries the main
 * view needs. Split from applyRouteToSignals so the signal side stays
 * synchronous (needed for the mount/popstate call sites below) while this
 * part awaits like any other loader. Unlike state/actions.ts's
 * selectAndLoadFeed, this deliberately skips markFeedVisited/
 * rememberFocusedEntryForCurrentFeed/prefetchNext -- a reload or
 * back/forward landing on a feed isn't "the reader chose to navigate here"
 * in the same sense a click is, so it doesn't feed navMemory's visited set
 * (which state/actions.ts's `a` shortcut reads back). */
export async function loadDataForRoute(view: MainView): Promise<void> {
  switch (view.kind) {
    case 'feed':
      await loadEntries(view.feedId)
      return
    case 'group':
      await loadGroupEntries(view.target)
      return
    case 'search':
      // A bare /search (no ?q=, e.g. a back/forward landing on the entry
      // openSearch itself pushed) has no query to load results for -- match
      // openSearch's own entries.value = [] so EntryPane's "キーワードを
      // 入力して検索してください" empty state shows instead of whatever
      // the previous view happened to leave in entries.
      if (view.query) await loadSearchEntries(view.query)
      else entries.value = []
      return
    case 'home':
    case 'manage':
      return
  }
}

/** Parses the current window.location into a route and applies it to the
 * signals -- shared by main.tsx's initial mount and its popstate handler. */
export function hydrateSignalsFromLocation(): RouteState {
  const route = decodeRoute(window.location)
  applyRouteToSignals(route)
  return route
}

/** Starts the one-way signals -> URL sync: on every change to any signal
 * this reads, re-encodes the route and replaceState's the URL if it
 * differs from what's already there. Never pushes a new history entry
 * itself -- entries are pushed only by pushMobileDetailNav (mobile back
 * gesture support); this only ever keeps the *current* entry's URL string
 * in sync with the current entry's URL string. Also starts a second effect
 * that keeps a folder/rating groupTarget's display label current once
 * subscriptions/folders finish loading -- decodeRoute (used by a
 * reload/bookmark) can only guess a label from whatever folders.value holds
 * at that instant, which is often still empty on first paint. Both are
 * started here (rather than at module scope) so a test that merely imports
 * this module doesn't get either running in the background, and so both
 * stop together via the single dispose function this returns for the
 * caller's cleanup. */
export function startUrlSync(): () => void {
  const disposeUrlSync = effect(() => {
    const url = encodeRoute(currentRouteFromSignals())
    const current = window.location.pathname + window.location.search
    if (url !== current) {
      window.history.replaceState(window.history.state, '', url)
    }
  })
  const disposeLabelFixup = effect(() => {
    const target = groupTarget.value
    if (!target || target.kind === 'today') return
    const label =
      target.kind === 'folder'
        ? folderLabel(target.folderId)
        : ratingLabel(target.rating)
    if (label !== target.label) {
      groupTarget.value = { ...target, label }
    }
  })
  return () => {
    disposeUrlSync()
    disposeLabelFixup()
  }
}
