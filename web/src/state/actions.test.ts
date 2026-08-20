// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../api/client'
import type { Entry, Pin, SubscriptionView } from '../api/types'
import {
  adjustRating,
  goToNextFeed,
  goToPreviousFeed,
  markAllRead,
  markFeedReadAll,
  moveFeedToFolder,
  openFeedManager,
  openSearch,
  refreshCurrentFeed,
  refreshFeed,
  runSearch,
  selectAndLoadFeed,
  selectGroup,
  setRating,
  togglePinFocused,
  unsubscribeCurrentFeed,
  unsubscribeFeed,
} from './actions'
import {
  entries,
  focusedIndex,
  loadEntries,
  loadGroupEntries,
  loadSearchEntries,
  markVisibleEntriesRead,
  prefetchNext,
  rememberFocusedEntryForCurrentFeed,
} from './entries'
import { hasVisitedFeed, resetNavMemory } from './navMemory'
import { pins } from './pins'
import type { GroupTarget } from './subscriptions'
import {
  adjacentFeedId,
  applySubscriptionPatch,
  ensureGroupExpanded,
  feedManagerMode,
  groupIdForFeed,
  groupTarget,
  isSameGroupTarget,
  loadSubscriptions,
  pushMobileDetailNav,
  removeSubscription,
  requestNavResetToHead,
  searchMode,
  searchQuery,
  selectedFeedId,
  selectFeed,
  subscriptions,
  todayUnreadCount,
} from './subscriptions'
import { feedManagerInitialOnlyErrors, toast } from './ui'

vi.mock('../api/client', () => ({
  refreshSubscription: vi.fn(),
  deleteSubscription: vi.fn(),
  readAll: vi.fn(),
  markAllEntriesRead: vi.fn(),
  patchSubscription: vi.fn(),
  addPin: vi.fn(),
  removePin: vi.fn(),
}))

vi.mock('./entries', async () => {
  const { signal } = await import('@preact/signals')
  return {
    entries: signal<Entry[]>([]),
    focusedIndex: signal(0),
    loadEntries: vi.fn(),
    loadGroupEntries: vi.fn(),
    loadSearchEntries: vi.fn(),
    markVisibleEntriesRead: vi.fn(),
    prefetchNext: vi.fn(),
    rememberFocusedEntryForCurrentFeed: vi.fn(),
  }
})

vi.mock('./pins', async () => {
  const { signal } = await import('@preact/signals')
  return { pins: signal<Pin[]>([]) }
})

vi.mock('./subscriptions', async () => {
  const { signal } = await import('@preact/signals')
  const subscriptions = signal<SubscriptionView[]>([])
  const selectedFeedId = signal<number | null>(null)
  return {
    subscriptions,
    selectedFeedId,
    adjacentFeedId: vi.fn<() => number | null>(() => null),
    ensureGroupExpanded: vi.fn(),
    groupIdForFeed: vi.fn<() => string | null>(() => null),
    groupTarget: signal<GroupTarget | null>(null),
    searchMode: signal(false),
    searchQuery: signal(''),
    feedManagerMode: signal(false),
    todayUnreadCount: signal(0),
    isSameGroupTarget: vi.fn((a: GroupTarget | null, b: GroupTarget) => {
      if (!a || a.kind !== b.kind) return false
      if (a.kind === 'folder' && b.kind === 'folder')
        return a.folderId === b.folderId
      if (a.kind === 'rating' && b.kind === 'rating')
        return a.rating === b.rating
      if (a.kind === 'today' && b.kind === 'today') return true
      return false
    }),
    loadSubscriptions: vi.fn(),
    pushMobileDetailNav: vi.fn(),
    removeSubscription: vi.fn((feedId: number) => {
      subscriptions.value = subscriptions.value.filter(
        (s) => s.feed_id !== feedId,
      )
    }),
    requestNavResetToHead: vi.fn(),
    selectFeed: vi.fn((feedId: number) => {
      selectedFeedId.value = feedId
      groupTargetMockRef.value = null
      searchModeMockRef.value = false
      feedManagerModeMockRef.value = false
    }),
    applySubscriptionPatch: vi.fn((view: SubscriptionView) => {
      subscriptions.value = subscriptions.value.map((s) =>
        s.feed_id === view.feed_id ? view : s,
      )
    }),
  }
})

// selectFeed's mock implementation above needs to reach the very same
// signal instances the factory returns -- closures over the destructured
// return value would grab a copy, so route through module-level refs
// assigned right after the mock module is first imported (see beforeEach).
let groupTargetMockRef: { value: unknown }
let searchModeMockRef: { value: boolean }
let feedManagerModeMockRef: { value: boolean }

function makeSub(overrides: Partial<SubscriptionView> = {}): SubscriptionView {
  return {
    feed_id: 1,
    feed_url: 'https://example.com/feed',
    title: 'Example',
    rating: 0,
    unread_count: 0,
    error_count: 0,
    next_fetch_at: 0,
    kind: 'rss',
    ...overrides,
  } as SubscriptionView
}

function makeEntry(overrides: Partial<Entry> = {}): Entry {
  return {
    id: 1,
    feed_id: 1,
    guid: 'guid-1',
    url: 'https://example.com/1',
    title: 'Entry',
    body: '',
    published_at: 0,
    updated_at: 0,
    fetched_at: 0,
    pinned: false,
    ...overrides,
  }
}

beforeEach(async () => {
  vi.clearAllMocks()
  const subsMod = await import('./subscriptions')
  groupTargetMockRef = subsMod.groupTarget
  searchModeMockRef = subsMod.searchMode
  feedManagerModeMockRef = subsMod.feedManagerMode
  subscriptions.value = []
  selectedFeedId.value = null
  groupTarget.value = null
  searchMode.value = false
  searchQuery.value = ''
  feedManagerMode.value = false
  todayUnreadCount.value = 0
  entries.value = []
  focusedIndex.value = 0
  pins.value = []
  toast.value = null
  feedManagerInitialOnlyErrors.value = false
  resetNavMemory()
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})

describe('selectAndLoadFeed', () => {
  it('marks visible entries read on an actual feed switch', async () => {
    selectedFeedId.value = 1
    await selectAndLoadFeed(2)
    expect(markVisibleEntriesRead).toHaveBeenCalled()
    expect(selectFeed).toHaveBeenCalledWith(2)
    expect(loadEntries).toHaveBeenCalledWith(2)
    expect(prefetchNext).toHaveBeenCalled()
  })

  it('does not mark read when re-selecting the already-selected feed', async () => {
    selectedFeedId.value = 2
    // selectFeed mock re-sets selectedFeedId synchronously to whatever is
    // passed, so pin it first to simulate "already selected".
    await selectAndLoadFeed(2)
    expect(markVisibleEntriesRead).not.toHaveBeenCalled()
    expect(rememberFocusedEntryForCurrentFeed).not.toHaveBeenCalled()
  })

  it('records the reading position and the visit on a switch', async () => {
    selectedFeedId.value = 1
    await selectAndLoadFeed(2)
    expect(rememberFocusedEntryForCurrentFeed).toHaveBeenCalled()
    expect(hasVisitedFeed(2)).toBe(true)
  })

  it('skips marking read when the caller opts out', async () => {
    selectedFeedId.value = 1
    await selectAndLoadFeed(2, { markVisibleRead: false })
    expect(markVisibleEntriesRead).not.toHaveBeenCalled()
    // The reading position is still recorded -- only the marking is skipped.
    expect(rememberFocusedEntryForCurrentFeed).toHaveBeenCalled()
  })
})

describe('goToNextFeed / goToPreviousFeed (the s and a keys)', () => {
  it('s walks forward without asking for visited feeds', () => {
    vi.mocked(adjacentFeedId).mockReturnValue(3)
    goToNextFeed()
    expect(adjacentFeedId).toHaveBeenCalledWith(1)
  })

  it('a walks backward asking for visited feeds too', () => {
    vi.mocked(adjacentFeedId).mockReturnValue(2)
    goToPreviousFeed()
    expect(adjacentFeedId).toHaveBeenCalledWith(-1, { includeVisited: true })
    expect(selectFeed).toHaveBeenCalledWith(2)
  })

  // Pressing `a` right after `s` landed here means "I wanted to keep reading
  // the previous feed" -- the entries on screen were never read, and marking
  // them would drop them out of the unread list for good.
  it('a does not mark the feed it leaves as read', () => {
    selectedFeedId.value = 3
    vi.mocked(adjacentFeedId).mockReturnValue(2)
    goToPreviousFeed()
    expect(markVisibleEntriesRead).not.toHaveBeenCalled()
  })

  it('s does mark the feed it leaves as read', () => {
    selectedFeedId.value = 1
    vi.mocked(adjacentFeedId).mockReturnValue(3)
    goToNextFeed()
    expect(markVisibleEntriesRead).toHaveBeenCalled()
  })

  it('does nothing when there is no adjacent feed', () => {
    vi.mocked(adjacentFeedId).mockReturnValue(null)
    goToNextFeed()
    goToPreviousFeed()
    expect(selectFeed).not.toHaveBeenCalled()
    expect(markVisibleEntriesRead).not.toHaveBeenCalled()
  })

  it('expands a folded sidebar group before selecting into it', () => {
    vi.mocked(groupIdForFeed).mockReturnValue('folder-3')
    vi.mocked(adjacentFeedId).mockReturnValue(2)
    goToPreviousFeed()
    expect(ensureGroupExpanded).toHaveBeenCalledWith('folder-3')
  })
})

describe('selectGroup', () => {
  const target = { kind: 'folder' as const, folderId: 1, label: 'Folder' }

  it('marks visible entries read when switching from a feed', async () => {
    selectedFeedId.value = 5
    await selectGroup(target)
    expect(markVisibleEntriesRead).toHaveBeenCalled()
    expect(pushMobileDetailNav).toHaveBeenCalledWith(null)
    expect(selectedFeedId.value).toBeNull()
    expect(groupTarget.value).toEqual(target)
    expect(searchMode.value).toBe(false)
    expect(feedManagerMode.value).toBe(false)
    expect(loadGroupEntries).toHaveBeenCalledWith(target)
  })

  it('marks visible entries read when switching from search mode', async () => {
    searchMode.value = true
    await selectGroup(target)
    expect(markVisibleEntriesRead).toHaveBeenCalled()
  })

  it('marks visible entries read when the target actually differs', async () => {
    groupTarget.value = { kind: 'folder', folderId: 2, label: 'Other' }
    await selectGroup(target)
    expect(isSameGroupTarget).toHaveBeenCalledWith(
      { kind: 'folder', folderId: 2, label: 'Other' },
      target,
    )
    expect(markVisibleEntriesRead).toHaveBeenCalled()
  })

  it('does not mark read when re-selecting the exact same group', async () => {
    groupTarget.value = target
    await selectGroup(target)
    expect(markVisibleEntriesRead).not.toHaveBeenCalled()
  })
})

describe('openSearch', () => {
  it('resets state and enters search mode', () => {
    selectedFeedId.value = 3
    groupTarget.value = { kind: 'today', label: 'Today' }
    feedManagerMode.value = true
    entries.value = [makeEntry()]

    openSearch()

    expect(markVisibleEntriesRead).toHaveBeenCalled()
    expect(pushMobileDetailNav).toHaveBeenCalledWith(null)
    expect(selectedFeedId.value).toBeNull()
    expect(groupTarget.value).toBeNull()
    expect(searchMode.value).toBe(true)
    expect(searchQuery.value).toBe('')
    expect(feedManagerMode.value).toBe(false)
    expect(entries.value).toEqual([])
  })

  it('does nothing when already in search mode', () => {
    searchMode.value = true
    searchQuery.value = 'kept'
    openSearch()
    expect(markVisibleEntriesRead).not.toHaveBeenCalled()
    expect(searchQuery.value).toBe('kept')
  })
})

describe('runSearch', () => {
  it('ignores a blank query', async () => {
    await runSearch('   ')
    expect(loadSearchEntries).not.toHaveBeenCalled()
    expect(searchMode.value).toBe(false)
  })

  it('trims and runs the query', async () => {
    await runSearch('  hello  ')
    expect(searchMode.value).toBe(true)
    expect(searchQuery.value).toBe('hello')
    expect(loadSearchEntries).toHaveBeenCalledWith('hello')
    expect(markVisibleEntriesRead).toHaveBeenCalled()
  })
})

describe('openFeedManager', () => {
  it('opens the manager with the requested error filter', () => {
    selectedFeedId.value = 1
    openFeedManager(true)
    expect(markVisibleEntriesRead).toHaveBeenCalled()
    expect(selectedFeedId.value).toBeNull()
    expect(groupTarget.value).toBeNull()
    expect(searchMode.value).toBe(false)
    expect(feedManagerInitialOnlyErrors.value).toBe(true)
    expect(feedManagerMode.value).toBe(true)
  })
})

describe('refreshFeed', () => {
  it('reloads subscriptions and, if selected, entries', async () => {
    vi.mocked(api.refreshSubscription).mockResolvedValue({ new_entries: 3 })
    selectedFeedId.value = 7
    const res = await refreshFeed(7)
    expect(api.refreshSubscription).toHaveBeenCalledWith(7)
    expect(loadSubscriptions).toHaveBeenCalled()
    expect(loadEntries).toHaveBeenCalledWith(7)
    expect(res).toEqual({ new_entries: 3 })
  })

  it('does not reload entries for a feed that is not selected', async () => {
    vi.mocked(api.refreshSubscription).mockResolvedValue({ new_entries: 0 })
    selectedFeedId.value = 1
    await refreshFeed(7)
    expect(loadEntries).not.toHaveBeenCalled()
  })
})

describe('refreshCurrentFeed', () => {
  it('does nothing when no feed is selected', async () => {
    selectedFeedId.value = null
    await refreshCurrentFeed()
    expect(api.refreshSubscription).not.toHaveBeenCalled()
    expect(requestNavResetToHead).not.toHaveBeenCalled()
  })

  it('refreshes the selected feed and resets nav to head', async () => {
    vi.mocked(api.refreshSubscription).mockResolvedValue({ new_entries: 0 })
    selectedFeedId.value = 9
    await refreshCurrentFeed()
    expect(api.refreshSubscription).toHaveBeenCalledWith(9)
    expect(requestNavResetToHead).toHaveBeenCalled()
  })
})

describe('unsubscribeFeed', () => {
  it('does nothing when the user cancels the confirm dialog', async () => {
    vi.mocked(window.confirm).mockReturnValue(false)
    subscriptions.value = [makeSub({ feed_id: 1 })]
    await unsubscribeFeed(1)
    expect(api.deleteSubscription).not.toHaveBeenCalled()
    expect(removeSubscription).not.toHaveBeenCalled()
  })

  it('deletes and removes the subscription on confirm', async () => {
    subscriptions.value = [makeSub({ feed_id: 1 })]
    selectedFeedId.value = 1
    entries.value = [makeEntry({ feed_id: 1 })]
    vi.mocked(api.deleteSubscription).mockResolvedValue(undefined)

    await unsubscribeFeed(1)

    expect(api.deleteSubscription).toHaveBeenCalledWith(1)
    expect(removeSubscription).toHaveBeenCalledWith(1)
    expect(entries.value).toEqual([])
  })

  it('leaves entries untouched when unsubscribing a non-selected feed', async () => {
    subscriptions.value = [makeSub({ feed_id: 1 })]
    selectedFeedId.value = 2
    const list = [makeEntry({ feed_id: 2 })]
    entries.value = list
    vi.mocked(api.deleteSubscription).mockResolvedValue(undefined)

    await unsubscribeFeed(1)

    expect(entries.value).toBe(list)
  })
})

describe('unsubscribeCurrentFeed', () => {
  it('does nothing when nothing is selected', async () => {
    selectedFeedId.value = null
    await unsubscribeCurrentFeed()
    expect(api.deleteSubscription).not.toHaveBeenCalled()
  })

  it('unsubscribes the selected feed', async () => {
    subscriptions.value = [makeSub({ feed_id: 4 })]
    selectedFeedId.value = 4
    vi.mocked(api.deleteSubscription).mockResolvedValue(undefined)
    await unsubscribeCurrentFeed()
    expect(api.deleteSubscription).toHaveBeenCalledWith(4)
  })
})

describe('markFeedReadAll', () => {
  it('does nothing for an unknown feed', async () => {
    subscriptions.value = []
    await markFeedReadAll(1)
    expect(api.readAll).not.toHaveBeenCalled()
  })

  it('does nothing when the user cancels', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, unread_count: 5 })]
    vi.mocked(window.confirm).mockReturnValue(false)
    await markFeedReadAll(1)
    expect(api.readAll).not.toHaveBeenCalled()
  })

  it('marks all read and clears entries when the feed is selected', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, unread_count: 5 })]
    selectedFeedId.value = 1
    entries.value = [makeEntry({ feed_id: 1 })]
    vi.mocked(api.readAll).mockResolvedValue({ marked_read: 5 })

    await markFeedReadAll(1)

    expect(api.readAll).toHaveBeenCalledWith(1, 0)
    expect(applySubscriptionPatch).toHaveBeenCalledWith(
      expect.objectContaining({ feed_id: 1, unread_count: 0 }),
    )
    expect(entries.value).toEqual([])
  })
})

describe('markAllRead', () => {
  it('does nothing when there is no unread', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, unread_count: 0 })]
    await markAllRead()
    expect(api.markAllEntriesRead).not.toHaveBeenCalled()
  })

  it('does nothing when the user cancels', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, unread_count: 2 })]
    vi.mocked(window.confirm).mockReturnValue(false)
    await markAllRead()
    expect(api.markAllEntriesRead).not.toHaveBeenCalled()
  })

  it('clears unread counts and entries outside of search mode', async () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, unread_count: 2 }),
      makeSub({ feed_id: 2, unread_count: 3 }),
    ]
    todayUnreadCount.value = 5
    entries.value = [makeEntry()]
    vi.mocked(api.markAllEntriesRead).mockResolvedValue({ marked_read: 5 })

    await markAllRead()

    expect(api.markAllEntriesRead).toHaveBeenCalled()
    expect(subscriptions.value.every((s) => s.unread_count === 0)).toBe(true)
    expect(todayUnreadCount.value).toBe(0)
    expect(entries.value).toEqual([])
  })

  it('leaves entries alone while in search mode', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, unread_count: 2 })]
    searchMode.value = true
    const list = [makeEntry()]
    entries.value = list
    vi.mocked(api.markAllEntriesRead).mockResolvedValue({ marked_read: 2 })

    await markAllRead()

    expect(entries.value).toBe(list)
  })
})

describe('setRating', () => {
  it('does nothing for an unknown feed', async () => {
    subscriptions.value = []
    await setRating(1, 3)
    expect(api.patchSubscription).not.toHaveBeenCalled()
  })

  it('sets a new rating optimistically then reconciles with the server', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, rating: 2 })]
    const updated = makeSub({ feed_id: 1, rating: 4 })
    vi.mocked(api.patchSubscription).mockResolvedValue(updated)

    await setRating(1, 4)

    expect(api.patchSubscription).toHaveBeenCalledWith(1, { rating: 4 })
    expect(subscriptions.value[0]).toEqual(updated)
  })

  it('toggles off when clicking the already-current rating', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, rating: 3 })]
    vi.mocked(api.patchSubscription).mockResolvedValue(
      makeSub({ feed_id: 1, rating: 0 }),
    )

    await setRating(1, 3)

    expect(api.patchSubscription).toHaveBeenCalledWith(1, { rating: 0 })
  })

  it('rolls back and shows a toast when the server rejects', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, rating: 2 })]
    vi.mocked(api.patchSubscription).mockRejectedValue(new Error('boom'))

    await setRating(1, 4)

    expect(subscriptions.value[0]?.rating).toBe(2)
    expect(toast.value).toEqual({ message: 'boom', variant: 'info' })
  })
})

describe('adjustRating', () => {
  it('does nothing for an unknown feed', async () => {
    subscriptions.value = []
    await adjustRating(1, 1)
    expect(api.patchSubscription).not.toHaveBeenCalled()
  })

  it('clamps to 5 at the top', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, rating: 5 })]
    await adjustRating(1, 1)
    expect(api.patchSubscription).not.toHaveBeenCalled()
  })

  it('clamps to 0 at the bottom', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, rating: 0 })]
    await adjustRating(1, -1)
    expect(api.patchSubscription).not.toHaveBeenCalled()
  })

  it('applies a clamped delta', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, rating: 4 })]
    vi.mocked(api.patchSubscription).mockResolvedValue(
      makeSub({ feed_id: 1, rating: 5 }),
    )
    await adjustRating(1, 3)
    expect(api.patchSubscription).toHaveBeenCalledWith(1, { rating: 5 })
  })
})

describe('moveFeedToFolder', () => {
  it('does nothing for an unknown feed', async () => {
    subscriptions.value = []
    await moveFeedToFolder(1, 2)
    expect(api.patchSubscription).not.toHaveBeenCalled()
  })

  it('does nothing when the folder is unchanged', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, folder_id: 2 })]
    await moveFeedToFolder(1, 2)
    expect(api.patchSubscription).not.toHaveBeenCalled()
  })

  it('treats a null target folder as unfiled (folder_id: 0)', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, folder_id: 2 })]
    vi.mocked(api.patchSubscription).mockResolvedValue(
      makeSub({ feed_id: 1, folder_id: undefined }),
    )
    await moveFeedToFolder(1, null)
    expect(api.patchSubscription).toHaveBeenCalledWith(1, { folder_id: 0 })
  })

  it('rolls back and shows a toast when the server rejects', async () => {
    subscriptions.value = [makeSub({ feed_id: 1, folder_id: 2 })]
    vi.mocked(api.patchSubscription).mockRejectedValue(new Error('boom'))
    await moveFeedToFolder(1, 3)
    expect(subscriptions.value[0]?.folder_id).toBe(2)
    expect(toast.value).toEqual({ message: 'boom', variant: 'info' })
  })
})

describe('togglePinFocused', () => {
  it('does nothing when there is no focused entry', async () => {
    entries.value = []
    focusedIndex.value = 0
    await togglePinFocused()
    expect(api.addPin).not.toHaveBeenCalled()
  })

  it('pins the focused entry optimistically', async () => {
    const entry = makeEntry({ id: 1, pinned: false })
    entries.value = [entry]
    focusedIndex.value = 0
    vi.mocked(api.addPin).mockResolvedValue({ entry_id: 1 })

    await togglePinFocused()

    expect(api.addPin).toHaveBeenCalledWith(1)
    expect(entries.value[0]?.pinned).toBe(true)
    expect(toast.value).toEqual({ message: 'pin しました', variant: 'info' })
  })

  it('unpins the focused entry and drops it from the pins list', async () => {
    const entry = makeEntry({ id: 1, pinned: true })
    entries.value = [entry]
    focusedIndex.value = 0
    pins.value = [{ id: 10, entry_id: 1 } as never]
    vi.mocked(api.removePin).mockResolvedValue(undefined)

    await togglePinFocused()

    expect(api.removePin).toHaveBeenCalledWith(1)
    expect(entries.value[0]?.pinned).toBe(false)
    expect(pins.value).toEqual([])
    expect(toast.value).toEqual({
      message: 'pin を解除しました',
      variant: 'info',
    })
  })

  it('rolls back the optimistic pin on failure', async () => {
    const entry = makeEntry({ id: 1, pinned: false })
    entries.value = [entry]
    focusedIndex.value = 0
    vi.mocked(api.addPin).mockRejectedValue(new Error('boom'))

    await togglePinFocused()

    expect(entries.value[0]?.pinned).toBe(false)
    expect(toast.value).toEqual({ message: 'boom', variant: 'info' })
  })
})
