// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../api/client'
import type { Entry } from '../api/types'
import {
  entries,
  entriesShowingReadFallback,
  focusedIndex,
  loadEntries,
  rememberFocusedEntryForCurrentFeed,
} from './entries'
import { recallFocusedEntry, resetNavMemory } from './navMemory'
import { selectedFeedId } from './subscriptions'

vi.mock('../api/client', () => ({
  listEntries: vi.fn(),
}))

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

/** A fully-read feed's worth of entries, ids 1..n -- the list `a` restores
 * a position into. */
function readEntries(count: number): Entry[] {
  return Array.from({ length: count }, (_, i) =>
    makeEntry({ id: i + 1, read_at: 100 }),
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  resetNavMemory()
  entries.value = []
  focusedIndex.value = 0
  entriesShowingReadFallback.value = false
  selectedFeedId.value = null
})

describe('rememberFocusedEntryForCurrentFeed', () => {
  it('records the entry the focus ring is on', () => {
    selectedFeedId.value = 7
    entries.value = readEntries(3)
    focusedIndex.value = 2
    rememberFocusedEntryForCurrentFeed()
    expect(recallFocusedEntry(7)).toBe(3)
  })

  it('records nothing when no feed is selected (group/search reading)', () => {
    selectedFeedId.value = null
    entries.value = readEntries(3)
    focusedIndex.value = 1
    rememberFocusedEntryForCurrentFeed()
    expect(recallFocusedEntry(7)).toBeUndefined()
  })
})

describe('loadEntries read fallback', () => {
  it('falls back to recent read entries when nothing is unread', async () => {
    selectedFeedId.value = 1
    vi.mocked(api.listEntries)
      .mockResolvedValueOnce({ entries: [] })
      .mockResolvedValueOnce({ entries: readEntries(3) })

    await loadEntries(1)

    expect(api.listEntries).toHaveBeenNthCalledWith(1, 1, {
      unread: true,
      limit: 200,
    })
    // Must match the unread query's limit: this is the list `a` restores a
    // remembered position into, and a short one silently loses the position
    // on exactly the heavily-read feeds `a` is most useful for.
    expect(api.listEntries).toHaveBeenNthCalledWith(2, 1, { limit: 200 })
    expect(entriesShowingReadFallback.value).toBe(true)
  })

  it('does not fall back when unread entries exist', async () => {
    selectedFeedId.value = 1
    vi.mocked(api.listEntries).mockResolvedValueOnce({
      entries: [makeEntry({ id: 1 })],
    })

    await loadEntries(1)

    expect(api.listEntries).toHaveBeenCalledTimes(1)
    expect(entriesShowingReadFallback.value).toBe(false)
  })
})

describe('loadEntries focus restore', () => {
  it('restores the remembered reading position', async () => {
    selectedFeedId.value = 1
    entries.value = readEntries(5)
    focusedIndex.value = 3
    rememberFocusedEntryForCurrentFeed()

    entries.value = []
    focusedIndex.value = 0
    vi.mocked(api.listEntries)
      .mockResolvedValueOnce({ entries: [] })
      .mockResolvedValueOnce({ entries: readEntries(5) })

    await loadEntries(1)

    expect(focusedIndex.value).toBe(3)
  })

  it('falls back to the top when the remembered entry is gone', async () => {
    selectedFeedId.value = 1
    entries.value = readEntries(5)
    focusedIndex.value = 4
    rememberFocusedEntryForCurrentFeed()

    entries.value = []
    vi.mocked(api.listEntries)
      .mockResolvedValueOnce({ entries: [] })
      .mockResolvedValueOnce({ entries: readEntries(3) })

    await loadEntries(1)

    expect(focusedIndex.value).toBe(0)
  })

  it('starts at the top for a feed with nothing remembered', async () => {
    selectedFeedId.value = 1
    vi.mocked(api.listEntries).mockResolvedValueOnce({
      entries: readEntries(3),
    })

    await loadEntries(1)

    expect(focusedIndex.value).toBe(0)
  })

  // Scrolling down to the remembered entry drags everything above it past
  // the pane's top edge, which useAutoMarkRead treats as read. Harmless on
  // the fully-read list this normally restores into, but a feed that picked
  // up newer entries since must not have them silently consumed.
  it('stays at the top when unread entries sit above the remembered one', async () => {
    selectedFeedId.value = 1
    entries.value = readEntries(3)
    focusedIndex.value = 2
    rememberFocusedEntryForCurrentFeed()

    entries.value = []
    vi.mocked(api.listEntries).mockResolvedValueOnce({
      entries: [
        makeEntry({ id: 10 }),
        makeEntry({ id: 11 }),
        makeEntry({ id: 3, read_at: 100 }),
      ],
    })

    await loadEntries(1)

    expect(focusedIndex.value).toBe(0)
  })
})
