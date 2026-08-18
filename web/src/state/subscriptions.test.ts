// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import type { Folder, SubscriptionView } from '../api/types'
import {
  addSubscription,
  adjacentFeedId,
  adjustTodayUnreadCount,
  adjustUnreadCount,
  applySubscriptionPatch,
  buildGroupsByFolder,
  buildGroupsByPriority,
  collapsedGroups,
  displayedFeedOrder,
  ERRORING_THRESHOLD,
  folders,
  groupUnreadCount,
  isErroringFeed,
  isSameGroupTarget,
  ratingLabel,
  removeSubscription,
  selectedFeedId,
  sidebarViewMode,
  subscriptions,
  subscriptionsInFolder,
  subscriptionsWithRating,
  todayUnreadCount,
  toggleGroupCollapsed,
} from './subscriptions'

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
