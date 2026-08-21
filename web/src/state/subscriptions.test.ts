// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Folder, SubscriptionView } from '../api/types'
import { hasVisitedFeed, markFeedVisited, resetNavMemory } from './navMemory'
import {
  addSubscription,
  adjacentFeedId,
  adjustTodayUnreadCount,
  adjustUnreadCount,
  applySubscriptionPatch,
  buildGroupsByFolder,
  buildGroupsByPriority,
  clearMobileBackPending,
  clearSelectedFeed,
  collapsedGroups,
  displayedFeedOrder,
  ERRORING_THRESHOLD,
  folders,
  groupTarget,
  groupUnreadCount,
  isErroringFeed,
  isSameGroupTarget,
  pushMobileDetailNav,
  ratingLabel,
  removeSubscription,
  requestNavResetToHead,
  resetSortSnapshot,
  selectedFeedId,
  sidebarViewMode,
  subscriptions,
  subscriptionsInFolder,
  subscriptionsWithRating,
  todayUnreadCount,
  toggleGroupCollapsed,
} from './subscriptions'

// Overrides testSetup.ts's always-false matchMedia stub for the duration of
// one test, so pushMobileDetailNav/clearSelectedFeed take their mobile
// branch instead of their (already well-covered) no-op desktop one.
function stubMobileViewport(): void {
  window.matchMedia = ((query: string) => ({
    matches: query.includes('max-width'),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia
}

function makeSub(overrides: Partial<SubscriptionView> = {}): SubscriptionView {
  return {
    feed_id: 1,
    feed_url: 'https://example.com/feed.xml',
    title: 'Example',
    folder_id: undefined,
    rating: 0,
    unread_count: 0,
    error_count: 0,
    last_entry_at: undefined,
    ...overrides,
  } as SubscriptionView
}

beforeEach(() => {
  subscriptions.value = []
  folders.value = []
  selectedFeedId.value = null
  sidebarViewMode.value = 'folder'
  collapsedGroups.value = {}
  todayUnreadCount.value = 0
  resetNavMemory()
  resetSortSnapshot()
})

describe('isErroringFeed', () => {
  it('is false below the threshold', () => {
    expect(
      isErroringFeed(makeSub({ error_count: ERRORING_THRESHOLD - 1 })),
    ).toBe(false)
  })

  it('is true at or above the threshold', () => {
    expect(isErroringFeed(makeSub({ error_count: ERRORING_THRESHOLD }))).toBe(
      true,
    )
  })
})

describe('isSameGroupTarget', () => {
  it('is false when a is null', () => {
    expect(isSameGroupTarget(null, { kind: 'today', label: 'Today' })).toBe(
      false,
    )
  })

  it('compares folder targets by folderId', () => {
    const a = { kind: 'folder' as const, folderId: 1, label: 'A' }
    expect(
      isSameGroupTarget(a, { kind: 'folder', folderId: 1, label: 'A2' }),
    ).toBe(true)
    expect(
      isSameGroupTarget(a, { kind: 'folder', folderId: 2, label: 'B' }),
    ).toBe(false)
  })

  it('compares rating targets by rating', () => {
    const a = { kind: 'rating' as const, rating: 3, label: 'A' }
    expect(
      isSameGroupTarget(a, { kind: 'rating', rating: 3, label: 'A2' }),
    ).toBe(true)
    expect(
      isSameGroupTarget(a, { kind: 'rating', rating: 4, label: 'B' }),
    ).toBe(false)
  })

  it('treats different kinds as different', () => {
    const a = { kind: 'folder' as const, folderId: null, label: 'A' }
    expect(isSameGroupTarget(a, { kind: 'today', label: 'Today' })).toBe(false)
  })
})

describe('subscriptionsInFolder / subscriptionsWithRating', () => {
  it('filters by folder id, treating undefined as unfiled (null)', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, folder_id: 1 }),
      makeSub({ feed_id: 2, folder_id: undefined }),
      makeSub({ feed_id: 3 }),
    ]
    expect(subscriptionsInFolder(1).map((s) => s.feed_id)).toEqual([1])
    expect(subscriptionsInFolder(null).map((s) => s.feed_id)).toEqual([2, 3])
  })

  it('filters by rating', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, rating: 5 }),
      makeSub({ feed_id: 2, rating: 0 }),
    ]
    expect(subscriptionsWithRating(5).map((s) => s.feed_id)).toEqual([1])
  })
})

describe('groupUnreadCount', () => {
  it('reads todayUnreadCount for the today target', () => {
    todayUnreadCount.value = 7
    expect(groupUnreadCount({ kind: 'today', label: 'Today' })).toBe(7)
  })

  it('sums unread_count across a folder group', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, folder_id: 1, unread_count: 3 }),
      makeSub({ feed_id: 2, folder_id: 1, unread_count: 4 }),
      makeSub({ feed_id: 3, folder_id: 2, unread_count: 100 }),
    ]
    expect(groupUnreadCount({ kind: 'folder', folderId: 1, label: 'A' })).toBe(
      7,
    )
  })

  it('sums unread_count across a rating group', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, rating: 5, unread_count: 2 }),
      makeSub({ feed_id: 2, rating: 5, unread_count: 5 }),
    ]
    expect(groupUnreadCount({ kind: 'rating', rating: 5, label: 'A' })).toBe(7)
  })
})

describe('ratingLabel', () => {
  it('renders 0 as 評価なし', () => {
    expect(ratingLabel(0)).toBe('評価なし')
  })

  it('renders filled/empty stars for the given rating', () => {
    expect(ratingLabel(3)).toBe('★★★☆☆')
    expect(ratingLabel(5)).toBe('★★★★★')
  })
})

describe('toggleGroupCollapsed', () => {
  it('flips the collapsed state for the given id', () => {
    toggleGroupCollapsed('folder-1')
    expect(collapsedGroups.value['folder-1']).toBe(true)
    toggleGroupCollapsed('folder-1')
    expect(collapsedGroups.value['folder-1']).toBe(false)
  })
})

describe('buildGroupsByFolder', () => {
  it('orders unread-before-read, newest-first within each half, unfiled last', () => {
    folders.value = [
      { id: 1, name: 'Tech', sort_order: 0 } as Folder,
      { id: 2, name: 'News', sort_order: 1 } as Folder,
    ]
    subscriptions.value = [
      makeSub({
        feed_id: 1,
        folder_id: 1,
        title: 'read-old',
        unread_count: 0,
        last_entry_at: 100,
      }),
      makeSub({
        feed_id: 2,
        folder_id: 1,
        title: 'unread-new',
        unread_count: 1,
        last_entry_at: 200,
      }),
      makeSub({
        feed_id: 3,
        folder_id: 1,
        title: 'unread-old',
        unread_count: 1,
        last_entry_at: 50,
      }),
      makeSub({ feed_id: 4, folder_id: undefined, title: 'unfiled' }),
    ]

    const groups = buildGroupsByFolder()
    // Folder groups with no subscriptions (News) are omitted.
    expect(groups.map((g) => g.name)).toEqual(['Tech', '(未分類)'])
    const tech = groups.find((g) => g.name === 'Tech')
    expect(tech?.subs.map((s) => s.title)).toEqual([
      'unread-new',
      'unread-old',
      'read-old',
    ])
  })

  it('falls back to title when hasUnread and lastEntryAt tie', () => {
    subscriptions.value = [
      makeSub({
        feed_id: 1,
        folder_id: undefined,
        title: 'Zebra',
        unread_count: 1,
        last_entry_at: 100,
      }),
      makeSub({
        feed_id: 2,
        folder_id: undefined,
        title: 'Alpha',
        unread_count: 1,
        last_entry_at: 100,
      }),
    ]
    const groups = buildGroupsByFolder()
    expect(groups[0].subs.map((s) => s.title)).toEqual(['Alpha', 'Zebra'])
  })
})

describe('buildGroupsByPriority', () => {
  it('groups by rating 5..0, omitting empty buckets', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, rating: 5, title: 'a' }),
      makeSub({ feed_id: 2, rating: 0, title: 'b' }),
    ]
    const groups = buildGroupsByPriority()
    expect(groups.map((g) => g.id)).toEqual(['rating-5', 'rating-0'])
  })
})

describe('displayedFeedOrder', () => {
  it('follows folder order in folder mode and priority order in priority mode', () => {
    folders.value = [{ id: 1, name: 'Tech', sort_order: 0 } as Folder]
    subscriptions.value = [
      makeSub({ feed_id: 1, folder_id: 1, rating: 0 }),
      makeSub({ feed_id: 2, folder_id: undefined, rating: 5 }),
    ]

    sidebarViewMode.value = 'folder'
    expect(displayedFeedOrder()).toEqual([1, 2])

    sidebarViewMode.value = 'priority'
    expect(displayedFeedOrder()).toEqual([2, 1])
  })
})

describe('adjacentFeedId', () => {
  beforeEach(() => {
    subscriptions.value = [
      makeSub({ feed_id: 1, folder_id: undefined, unread_count: 1 }),
      makeSub({ feed_id: 2, folder_id: undefined, unread_count: 0 }),
      makeSub({ feed_id: 3, folder_id: undefined, unread_count: 1 }),
    ]
  })

  it('returns null when nothing has unread entries', () => {
    subscriptions.value = subscriptions.value.map((s) => ({
      ...s,
      unread_count: 0,
    }))
    expect(adjacentFeedId(1)).toBeNull()
  })

  it('starts from the top of the list when nothing is selected', () => {
    expect(adjacentFeedId(1)).toBe(1)
  })

  it('skips fully-read feeds when moving forward', () => {
    selectedFeedId.value = 1
    expect(adjacentFeedId(1)).toBe(3)
  })

  it('returns null past the end of the list', () => {
    selectedFeedId.value = 3
    expect(adjacentFeedId(1)).toBeNull()
  })

  it('walks backward skipping fully-read feeds', () => {
    selectedFeedId.value = 3
    expect(adjacentFeedId(-1)).toBe(1)
  })
})

/** Builds feeds 1..n in that sidebar order and then reads the ones whose
 * entry in `unreadCounts` is 0 down to zero unread.
 *
 * Seeding them all as unread first matters: compareFeedsBySnapshot puts
 * unread feeds ahead of read-through ones, but only against the *frozen*
 * sort key (see feedSortSnapshot), so a feed read during a session keeps
 * its sidebar position. That frozen ordering is the one s/a walk, and
 * building the list with fully-read feeds up front would instead test an
 * ordering the reader never sees. */
function seedFrozenOrder(unreadCounts: number[]): void {
  unreadCounts.forEach((_, i) => {
    addSubscription(
      makeSub({
        feed_id: i + 1,
        title: `Feed ${i + 1}`,
        last_entry_at: 1000 - i,
        unread_count: 1,
      }),
    )
  })
  unreadCounts.forEach((unread, i) => {
    if (unread === 0) adjustUnreadCount(i + 1, -1)
  })
}

describe('adjacentFeedId with includeVisited (the `a` key)', () => {
  // `a`'s whole reason for existing: reading feed 2 to the end drops its
  // unread_count to 0, and a plain backward walk steps straight over it to
  // feed 1 -- two feeds back from where the reader actually was.
  it('lands on a fully-read but visited feed', () => {
    seedFrozenOrder([1, 0, 1])
    markFeedVisited(2)
    selectedFeedId.value = 3
    expect(adjacentFeedId(-1, { includeVisited: true })).toBe(2)
    expect(adjacentFeedId(-1)).toBe(1)
  })

  it('still skips fully-read feeds that were never visited', () => {
    seedFrozenOrder([1, 0, 1])
    selectedFeedId.value = 3
    expect(adjacentFeedId(-1, { includeVisited: true })).toBe(1)
  })

  // `s` must keep burning through unreads -- stopping on read feeds on the
  // way forward would defeat the point of holding the key down -- so it
  // (and prefetchNext, which shares this call) never passes the option.
  it('leaves forward navigation alone when the option is not passed', () => {
    seedFrozenOrder([1, 0, 1])
    markFeedVisited(2)
    selectedFeedId.value = 1
    expect(adjacentFeedId(1)).toBe(3)
  })

  // The head scan is the shared "nothing selected" entry point for s and a
  // alike; honoring visited there would park `a` on the same long-finished
  // feed at the top of the sidebar every time.
  it('is ignored on the head scan when nothing is selected', () => {
    seedFrozenOrder([0, 1])
    markFeedVisited(1)
    selectedFeedId.value = null
    expect(adjacentFeedId(-1, { includeVisited: true })).toBe(2)
  })

  it('is ignored on the head scan after requestNavResetToHead', () => {
    seedFrozenOrder([0, 1, 1])
    markFeedVisited(1)
    selectedFeedId.value = 3
    requestNavResetToHead()
    expect(adjacentFeedId(-1, { includeVisited: true })).toBe(2)
  })

  it('returns null when only unvisited fully-read feeds remain behind', () => {
    seedFrozenOrder([0, 1])
    selectedFeedId.value = 2
    expect(adjacentFeedId(-1, { includeVisited: true })).toBeNull()
  })

  it('drops a feed from the visited set when it is unsubscribed', () => {
    seedFrozenOrder([1])
    markFeedVisited(1)
    removeSubscription(1)
    expect(hasVisitedFeed(1)).toBe(false)
  })
})

describe('applySubscriptionPatch / removeSubscription / addSubscription', () => {
  it('applySubscriptionPatch replaces the matching row', () => {
    subscriptions.value = [makeSub({ feed_id: 1, title: 'old' })]
    applySubscriptionPatch(makeSub({ feed_id: 1, title: 'new' }))
    expect(subscriptions.value[0].title).toBe('new')
  })

  it('removeSubscription drops the matching row', () => {
    subscriptions.value = [makeSub({ feed_id: 1 }), makeSub({ feed_id: 2 })]
    removeSubscription(1)
    expect(subscriptions.value.map((s) => s.feed_id)).toEqual([2])
  })

  it('addSubscription appends a new feed', () => {
    subscriptions.value = [makeSub({ feed_id: 1 })]
    addSubscription(makeSub({ feed_id: 2 }))
    expect(subscriptions.value.map((s) => s.feed_id)).toEqual([1, 2])
  })

  it('addSubscription replaces an existing row instead of duplicating it', () => {
    subscriptions.value = [makeSub({ feed_id: 1, title: 'old' })]
    addSubscription(makeSub({ feed_id: 1, title: 'new' }))
    expect(subscriptions.value.map((s) => s.title)).toEqual(['new'])
  })
})

describe('adjustUnreadCount / adjustTodayUnreadCount', () => {
  it('adjustUnreadCount clamps at 0', () => {
    subscriptions.value = [makeSub({ feed_id: 1, unread_count: 1 })]
    adjustUnreadCount(1, -5)
    expect(subscriptions.value[0].unread_count).toBe(0)
  })

  it('adjustTodayUnreadCount clamps at 0', () => {
    todayUnreadCount.value = 1
    adjustTodayUnreadCount(-5)
    expect(todayUnreadCount.value).toBe(0)
  })
})

describe('pushMobileDetailNav / clearSelectedFeed (mobile back gesture)', () => {
  const originalMatchMedia = window.matchMedia

  beforeEach(() => {
    stubMobileViewport()
    window.history.replaceState(null, '', '/')
    clearMobileBackPending()
  })

  afterEach(() => {
    window.matchMedia = originalMatchMedia
    window.history.replaceState(null, '', '/')
    vi.restoreAllMocks()
  })

  it('pushes a marked entry from the list, replaces it when already in detail', () => {
    const pushSpy = vi.spyOn(window.history, 'pushState')
    const replaceSpy = vi.spyOn(window.history, 'replaceState')

    pushMobileDetailNav()
    expect(pushSpy).toHaveBeenCalledWith({ feedlaNav: true }, '')
    expect(replaceSpy).not.toHaveBeenCalled()

    selectedFeedId.value = 1
    pushSpy.mockClear()
    replaceSpy.mockClear()
    pushMobileDetailNav()
    expect(replaceSpy).toHaveBeenCalledWith({ feedlaNav: true }, '')
    expect(pushSpy).not.toHaveBeenCalled()
  })

  it('goes through history.back() when the current entry is one it pushed', () => {
    pushMobileDetailNav()
    selectedFeedId.value = 5
    const backSpy = vi
      .spyOn(window.history, 'back')
      .mockImplementation(() => {})

    clearSelectedFeed()

    expect(backSpy).toHaveBeenCalledTimes(1)
    // The actual clear happens via the resulting popstate (main.tsx), not
    // synchronously here -- see clearSelectedFeed's own comment.
    expect(selectedFeedId.value).toBe(5)
  })

  it('does not call history.back() a second time before the pop lands (double-tap guard)', () => {
    pushMobileDetailNav()
    selectedFeedId.value = 5
    const backSpy = vi
      .spyOn(window.history, 'back')
      .mockImplementation(() => {})

    clearSelectedFeed()
    clearSelectedFeed()

    expect(backSpy).toHaveBeenCalledTimes(1)
  })

  it('clears the signals synchronously instead of calling history.back() when the tab landed on this detail view directly (deep link/reload, no feedla-pushed entry)', () => {
    // No pushMobileDetailNav call -- e.g. hydrateSignalsFromLocation set
    // selectedFeedId straight from a /feed/12 URL on the tab's very first
    // navigation, so window.history.state carries no feedlaNav marker.
    selectedFeedId.value = 5
    groupTarget.value = null
    const backSpy = vi
      .spyOn(window.history, 'back')
      .mockImplementation(() => {})

    clearSelectedFeed()

    expect(backSpy).not.toHaveBeenCalled()
    expect(selectedFeedId.value).toBeNull()
  })
})
