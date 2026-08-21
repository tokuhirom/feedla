// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  feedManagerErrorNeedle,
  feedManagerKindFilter,
  feedManagerMinErrorCount,
  feedManagerOnlyErrors,
  feedManagerQuery,
  feedManagerUrlNeedle,
} from './feedManager'
import {
  feedManagerMode,
  folders,
  groupTarget,
  searchMode,
  searchQuery,
  selectedFeedId,
} from './subscriptions'
import {
  applyRouteToSignals,
  currentRouteFromSignals,
  decodeRoute,
  encodeRoute,
  hydrateSignalsFromLocation,
  type RouteState,
  startUrlSync,
} from './url'

function locFor(url: string): { pathname: string; search: string } {
  const u = new URL(url, 'http://localhost')
  return { pathname: u.pathname, search: u.search }
}

beforeEach(() => {
  folders.value = []
  selectedFeedId.value = null
  groupTarget.value = null
  searchMode.value = false
  searchQuery.value = ''
  feedManagerMode.value = false
  feedManagerKindFilter.value = 'all'
  feedManagerQuery.value = ''
  feedManagerOnlyErrors.value = false
  feedManagerMinErrorCount.value = ''
  feedManagerUrlNeedle.value = ''
  feedManagerErrorNeedle.value = ''
})

describe('encodeRoute', () => {
  it('encodes home as /', () => {
    expect(encodeRoute({ view: { kind: 'home' }, overlays: [] })).toBe('/')
  })

  it('encodes a selected feed', () => {
    expect(
      encodeRoute({ view: { kind: 'feed', feedId: 42 }, overlays: [] }),
    ).toBe('/feed/42')
  })

  it('encodes a folder group, unfiled as a sentinel', () => {
    expect(
      encodeRoute({
        view: {
          kind: 'group',
          target: { kind: 'folder', folderId: 3, label: 'Tech' },
        },
        overlays: [],
      }),
    ).toBe('/group/folder/3')
    expect(
      encodeRoute({
        view: {
          kind: 'group',
          target: { kind: 'folder', folderId: null, label: '(未分類)' },
        },
        overlays: [],
      }),
    ).toBe('/group/folder/unfiled')
  })

  it('encodes a rating group', () => {
    expect(
      encodeRoute({
        view: {
          kind: 'group',
          target: { kind: 'rating', rating: 4, label: '★★★★☆' },
        },
        overlays: [],
      }),
    ).toBe('/group/rating/4')
  })

  it('encodes the today group', () => {
    expect(
      encodeRoute({
        view: {
          kind: 'group',
          target: { kind: 'today', label: 'Today' },
        },
        overlays: [],
      }),
    ).toBe('/group/today')
  })

  it('omits ?q for an empty search, includes it otherwise', () => {
    expect(
      encodeRoute({ view: { kind: 'search', query: '' }, overlays: [] }),
    ).toBe('/search')
    expect(
      encodeRoute({ view: { kind: 'search', query: 'hello' }, overlays: [] }),
    ).toBe('/search?q=hello')
  })

  it('omits every manage param at its default value', () => {
    expect(
      encodeRoute({
        view: {
          kind: 'manage',
          filters: {
            kindFilter: 'all',
            query: '',
            onlyErrors: false,
            minErrorCount: '',
            urlNeedle: '',
            errorNeedle: '',
          },
        },
        overlays: [],
      }),
    ).toBe('/manage')
  })

  it('includes only the non-default manage filters', () => {
    expect(
      encodeRoute({
        view: {
          kind: 'manage',
          filters: {
            kindFilter: 'selector',
            query: 'foo',
            onlyErrors: true,
            minErrorCount: '3',
            urlNeedle: 'bar',
            errorNeedle: 'baz',
          },
        },
        overlays: [],
      }),
    ).toBe('/manage?kind=selector&q=foo&err=1&minErr=3&url=bar&errMsg=baz')
  })

  it('appends open overlays as a comma-separated ov param', () => {
    expect(
      encodeRoute({
        view: { kind: 'home' },
        overlays: ['stats', 'pins'],
      }),
    ).toBe('/?ov=stats%2Cpins')
  })
})

describe('decodeRoute', () => {
  it('falls back to home for an unknown path', () => {
    expect(decodeRoute(locFor('/nope'))).toEqual<RouteState>({
      view: { kind: 'home' },
      overlays: [],
    })
  })

  it('falls back to home for a non-numeric feed id', () => {
    expect(decodeRoute(locFor('/feed/abc'))).toEqual<RouteState>({
      view: { kind: 'home' },
      overlays: [],
    })
  })

  it('decodes a feed path', () => {
    expect(decodeRoute(locFor('/feed/42'))).toEqual<RouteState>({
      view: { kind: 'feed', feedId: 42 },
      overlays: [],
    })
  })

  it('resolves a folder label from the folders signal', () => {
    folders.value = [{ id: 3, name: 'Tech', sort_order: 0 }]
    expect(decodeRoute(locFor('/group/folder/3'))).toEqual<RouteState>({
      view: {
        kind: 'group',
        target: { kind: 'folder', folderId: 3, label: 'Tech' },
      },
      overlays: [],
    })
  })

  it('decodes the unfiled folder sentinel', () => {
    expect(decodeRoute(locFor('/group/folder/unfiled'))).toEqual<RouteState>({
      view: {
        kind: 'group',
        target: { kind: 'folder', folderId: null, label: '(未分類)' },
      },
      overlays: [],
    })
  })

  it('falls back to home for an out-of-range rating', () => {
    expect(decodeRoute(locFor('/group/rating/9'))).toEqual<RouteState>({
      view: { kind: 'home' },
      overlays: [],
    })
  })

  it('decodes the today group', () => {
    expect(decodeRoute(locFor('/group/today'))).toEqual<RouteState>({
      view: { kind: 'group', target: { kind: 'today', label: 'Today' } },
      overlays: [],
    })
  })

  it('decodes a search query', () => {
    expect(decodeRoute(locFor('/search?q=hello'))).toEqual<RouteState>({
      view: { kind: 'search', query: 'hello' },
      overlays: [],
    })
  })

  it('falls back an invalid kind param to all', () => {
    expect(decodeRoute(locFor('/manage?kind=bogus'))).toEqual<RouteState>({
      view: {
        kind: 'manage',
        filters: {
          kindFilter: 'all',
          query: '',
          onlyErrors: false,
          minErrorCount: '',
          urlNeedle: '',
          errorNeedle: '',
        },
      },
      overlays: [],
    })
  })

  it('decodes every manage filter', () => {
    expect(
      decodeRoute(
        locFor('/manage?kind=selector&q=foo&err=1&minErr=3&url=bar&errMsg=baz'),
      ),
    ).toEqual<RouteState>({
      view: {
        kind: 'manage',
        filters: {
          kindFilter: 'selector',
          query: 'foo',
          onlyErrors: true,
          minErrorCount: '3',
          urlNeedle: 'bar',
          errorNeedle: 'baz',
        },
      },
      overlays: [],
    })
  })

  it('drops unknown overlay ids but keeps known ones', () => {
    expect(decodeRoute(locFor('/?ov=stats,bogus,pins')).overlays).toEqual([
      'stats',
      'pins',
    ])
  })
})

describe('currentRouteFromSignals / applyRouteToSignals round-trip', () => {
  it('round-trips a selected feed through encode/decode', () => {
    selectedFeedId.value = 7
    const route = currentRouteFromSignals()
    const decoded = decodeRoute(locFor(encodeRoute(route)))
    applyRouteToSignals(decoded)
    expect(selectedFeedId.value).toBe(7)
    expect(groupTarget.value).toBeNull()
  })

  it('round-trips フィード管理 filters through encode/decode', () => {
    feedManagerMode.value = true
    feedManagerKindFilter.value = 'pagewatch'
    feedManagerQuery.value = 'foo'
    feedManagerOnlyErrors.value = true
    feedManagerMinErrorCount.value = '2'
    feedManagerUrlNeedle.value = 'bar'
    feedManagerErrorNeedle.value = 'baz'

    const route = currentRouteFromSignals()
    const decoded = decodeRoute(locFor(encodeRoute(route)))
    applyRouteToSignals(decoded)

    expect(feedManagerMode.value).toBe(true)
    expect(feedManagerKindFilter.value).toBe('pagewatch')
    expect(feedManagerQuery.value).toBe('foo')
    expect(feedManagerOnlyErrors.value).toBe(true)
    expect(feedManagerMinErrorCount.value).toBe('2')
    expect(feedManagerUrlNeedle.value).toBe('bar')
    expect(feedManagerErrorNeedle.value).toBe('baz')
  })

  it('round-trips open overlays', () => {
    const route: RouteState = {
      view: { kind: 'home' },
      overlays: ['stats', 'pins'],
    }
    const decoded = decodeRoute(locFor(encodeRoute(route)))
    applyRouteToSignals(decoded)
    const after = currentRouteFromSignals()
    expect(new Set(after.overlays)).toEqual(new Set(['stats', 'pins']))
  })

  it('turns feedManagerMode off when leaving /manage', () => {
    applyRouteToSignals({
      view: {
        kind: 'manage',
        filters: {
          kindFilter: 'selector',
          query: 'foo',
          onlyErrors: true,
          minErrorCount: '2',
          urlNeedle: 'bar',
          errorNeedle: 'baz',
        },
      },
      overlays: [],
    })
    applyRouteToSignals({ view: { kind: 'home' }, overlays: [] })
    expect(feedManagerMode.value).toBe(false)
    expect(selectedFeedId.value).toBeNull()
  })
})

describe('hydrateSignalsFromLocation', () => {
  it('applies the current window.location to the signals', () => {
    window.history.replaceState(null, '', '/feed/11')
    const route = hydrateSignalsFromLocation()
    expect(route.view).toEqual({ kind: 'feed', feedId: 11 })
    expect(selectedFeedId.value).toBe(11)
  })
})

describe('startUrlSync', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('replaces the URL whenever a watched signal changes', () => {
    window.history.replaceState(null, '', '/')
    const dispose = startUrlSync()
    try {
      selectedFeedId.value = 5
      expect(window.location.pathname).toBe('/feed/5')
      selectedFeedId.value = null
      expect(window.location.pathname).toBe('/')
    } finally {
      dispose()
    }
  })

  it('stops updating the URL once disposed', () => {
    window.history.replaceState(null, '', '/')
    const dispose = startUrlSync()
    dispose()
    selectedFeedId.value = 9
    expect(window.location.pathname).toBe('/')
  })

  it('never pushes a new entry, and preserves history.state (the mobile back-gesture marker) across every URL rewrite', () => {
    window.history.replaceState({ feedlaNav: true }, '', '/')
    const pushSpy = vi.spyOn(window.history, 'pushState')
    const dispose = startUrlSync()
    try {
      selectedFeedId.value = 3
      groupTarget.value = { kind: 'rating', rating: 2, label: 'x' }
      searchMode.value = true
      expect(pushSpy).not.toHaveBeenCalled()
      expect(window.history.state).toEqual({ feedlaNav: true })
    } finally {
      dispose()
    }
  })

  it('resolves a folder groupTarget label once folders finish loading', () => {
    window.history.replaceState(null, '', '/')
    const dispose = startUrlSync()
    try {
      groupTarget.value = { kind: 'folder', folderId: 3, label: '(未分類)' }
      expect(groupTarget.value?.label).toBe('(未分類)')
      folders.value = [{ id: 3, name: 'Tech', sort_order: 0 }]
      expect(groupTarget.value?.label).toBe('Tech')
    } finally {
      dispose()
    }
  })
})
