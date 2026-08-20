// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../api/client'
import type { Entry, SubscriptionView } from '../api/types'
import { entries, entriesShowingReadFallback } from '../state/entries'
import { folders, selectedFeedId, subscriptions } from '../state/subscriptions'
import { feedDetailOpen, feedManagerInitialOnlyErrors } from '../state/ui'
import { FeedManagerPane } from './FeedManagerPane'

vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof api>()),
  listEntries: vi.fn(),
}))

// state/entries.ts resets the pane's scrollTop on every load; jsdom's Element
// has no scrollTo, so stub it rather than letting the load throw.
Element.prototype.scrollTo = () => {}

function makeSub(overrides: Partial<SubscriptionView> = {}): SubscriptionView {
  return {
    feed_id: 1,
    feed_url: 'https://example.com/feed.xml',
    title: 'Example Feed',
    rating: 0,
    unread_count: 0,
    error_count: 0,
    next_fetch_at: 0,
    kind: 'feed',
    ...overrides,
  } as SubscriptionView
}

beforeEach(() => {
  vi.clearAllMocks()
  subscriptions.value = []
  folders.value = []
  feedManagerInitialOnlyErrors.value = false
  selectedFeedId.value = null
  entries.value = []
  entriesShowingReadFallback.value = false
  feedDetailOpen.value = false
})

afterEach(() => {
  cleanup()
})

describe('FeedManagerPane basic search filter', () => {
  it('shows every subscription with no filter applied', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Tech Blog' }),
      makeSub({ feed_id: 2, title: 'News Site' }),
    ]
    render(<FeedManagerPane />)
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('2 / 2 件')).toBeInTheDocument()
  })

  it('filters by title, case-insensitively', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Tech Blog' }),
      makeSub({ feed_id: 2, title: 'News Site' }),
    ]
    render(<FeedManagerPane />)
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'tech' },
    })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('Tech Blog')).toBeInTheDocument()
  })

  it('filters by feed_url when the title does not match', () => {
    subscriptions.value = [
      makeSub({
        feed_id: 1,
        title: 'Tech Blog',
        feed_url: 'https://a.example/feed',
      }),
      makeSub({
        feed_id: 2,
        title: 'News Site',
        feed_url: 'https://b.example/feed',
      }),
    ]
    render(<FeedManagerPane />)
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'b.example' },
    })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('News Site')).toBeInTheDocument()
  })

  it('filters by site_url', () => {
    subscriptions.value = [
      makeSub({
        feed_id: 1,
        title: 'Tech Blog',
        site_url: 'https://blog.example',
      }),
      makeSub({
        feed_id: 2,
        title: 'News Site',
        site_url: 'https://news.example',
      }),
    ]
    render(<FeedManagerPane />)
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'blog.example' },
    })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('Tech Blog')).toBeInTheDocument()
  })

  it('shows the empty state when nothing matches', () => {
    subscriptions.value = [makeSub({ feed_id: 1, title: 'Tech Blog' })]
    render(<FeedManagerPane />)
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'nomatch' },
    })
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
    expect(screen.getByText('該当するフィードはありません')).toBeInTheDocument()
  })
})

describe('FeedManagerPane kind filter', () => {
  function mixedSubs(): SubscriptionView[] {
    return [
      makeSub({ feed_id: 1, title: 'Plain Feed', kind: 'feed' }),
      makeSub({ feed_id: 2, title: 'Watched Page', kind: 'pagewatch' }),
      makeSub({ feed_id: 3, title: 'Scraped List', kind: 'selector' }),
      makeSub({ feed_id: 4, title: 'Another List', kind: 'selector' }),
    ]
  }

  it('counts each kind on its button', () => {
    subscriptions.value = mixedSubs()
    render(<FeedManagerPane />)
    expect(
      screen.getByRole('button', { name: /記事一覧抽出/ }),
    ).toHaveTextContent('記事一覧抽出 (2)')
    expect(
      screen.getByRole('button', { name: /ページ監視/ }),
    ).toHaveTextContent('ページ監視 (1)')
    expect(screen.getByRole('button', { name: /すべて/ })).toHaveTextContent(
      'すべて (4)',
    )
  })

  it('narrows to a single kind', () => {
    subscriptions.value = mixedSubs()
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /記事一覧抽出/ }))
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('Scraped List')).toBeInTheDocument()
    expect(screen.getByText('Another List')).toBeInTheDocument()
    expect(screen.getByText('2 / 4 件')).toBeInTheDocument()
  })

  it('combines the kind filter with the text query', () => {
    subscriptions.value = mixedSubs()
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /記事一覧抽出/ }))
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'another' },
    })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('Another List')).toBeInTheDocument()
  })

  it('combines the kind filter with the ⚠ エラーのみ view', () => {
    subscriptions.value = [
      makeSub({
        feed_id: 1,
        title: 'Broken Feed',
        kind: 'feed',
        error_count: 3,
      }),
      makeSub({
        feed_id: 2,
        title: 'Broken List',
        kind: 'selector',
        error_count: 3,
      }),
      makeSub({ feed_id: 3, title: 'Healthy List', kind: 'selector' }),
    ]
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /⚠ エラーのみ/ }))
    fireEvent.click(screen.getByRole('button', { name: /記事一覧抽出/ }))
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('Broken List')).toBeInTheDocument()
  })

  it('returns to every feed via すべて', () => {
    subscriptions.value = mixedSubs()
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /ページ監視/ }))
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: /すべて/ }))
    expect(screen.getAllByRole('listitem')).toHaveLength(4)
  })

  it('disables a kind button with no feeds, but keeps the active one clickable', () => {
    subscriptions.value = [makeSub({ feed_id: 1, kind: 'feed' })]
    render(<FeedManagerPane />)
    expect(screen.getByRole('button', { name: /記事一覧抽出/ })).toBeDisabled()

    // The active filter must stay enabled even at zero, or unsubscribing the
    // last feed of a kind would strand the pane on an un-clickable button.
    subscriptions.value = [makeSub({ feed_id: 1, kind: 'selector' })]
    fireEvent.click(screen.getByRole('button', { name: /記事一覧抽出/ }))
    subscriptions.value = [makeSub({ feed_id: 1, kind: 'feed' })]
    expect(
      screen.getByRole('button', { name: /記事一覧抽出/ }),
    ).not.toBeDisabled()
  })
})

describe('FeedManagerPane error view', () => {
  it('disables the ⚠エラーのみ toggle when nothing is erroring', () => {
    subscriptions.value = [makeSub({ feed_id: 1, error_count: 0 })]
    render(<FeedManagerPane />)
    expect(screen.getByRole('button', { name: '⚠ エラーのみ' })).toBeDisabled()
  })

  it('narrows to only erroring feeds and reveals the extra filters', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Healthy', error_count: 0 }),
      makeSub({
        feed_id: 2,
        title: 'Broken',
        error_count: 3,
        last_error: 'timeout',
      }),
    ]
    render(<FeedManagerPane />)

    const toggle = screen.getByRole('button', { name: /⚠ エラーのみ/ })
    expect(toggle).toHaveTextContent('⚠ エラーのみ (1)')
    fireEvent.click(toggle)

    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('Broken')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('URL部分一致')).toBeInTheDocument()
  })

  it('combines the error-count threshold filter with the basic search query', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Broken A', error_count: 3 }),
      makeSub({ feed_id: 2, title: 'Broken B', error_count: 5 }),
    ]
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /⚠ エラーのみ/ }))
    fireEvent.input(screen.getByPlaceholderText('エラー回数以上'), {
      target: { value: '4' },
    })
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'broken' },
    })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('Broken B')).toBeInTheDocument()
  })

  it('resets the error-view filters and selection when toggled off then back on', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Broken A', error_count: 3 }),
      makeSub({ feed_id: 2, title: 'Broken B', error_count: 3 }),
    ]
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /⚠ エラーのみ/ }))
    fireEvent.input(screen.getByPlaceholderText('URL部分一致'), {
      target: { value: 'example' },
    })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    expect(screen.getByText(/選択中/)).toHaveTextContent('1 件選択中')

    // Off then back on: the error-only view's own filters/selection reset,
    // but this is a UI toggle, not an unmount -- confirms state doesn't leak.
    fireEvent.click(screen.getByRole('button', { name: /⚠ エラーのみ/ }))
    fireEvent.click(screen.getByRole('button', { name: /⚠ エラーのみ/ }))

    expect(screen.getByPlaceholderText('URL部分一致')).toHaveValue('')
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText(/選択中/)).toHaveTextContent('0 件選択中')
  })
})

describe('FeedManagerPane 詳細 button', () => {
  function makeEntry(overrides: Partial<Entry> = {}): Entry {
    return {
      id: 1,
      feed_id: 3,
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

  it('loads the feed behind the dialog, falling back to read entries when nothing is unread', async () => {
    subscriptions.value = [
      makeSub({ feed_id: 3, title: 'Tech Blog', unread_count: 0 }),
    ]
    const read = [makeEntry({ id: 11, read_at: 100 })]
    vi.mocked(api.listEntries)
      .mockResolvedValueOnce({ entries: [] }) // unread: none left
      .mockResolvedValueOnce({ entries: read }) // read fallback
    render(<FeedManagerPane />)

    fireEvent.click(screen.getByRole('button', { name: '詳細' }))

    expect(feedDetailOpen.value).toBe(true)
    expect(selectedFeedId.value).toBe(3)
    // Closing the dialog drops back to this list, so it must not be empty
    // just because the feed has no unread entries left.
    await waitFor(() => {
      expect(entries.value).toEqual(read)
    })
    expect(entriesShowingReadFallback.value).toBe(true)
  })
})

describe('FeedManagerPane selection across filter changes', () => {
  it('keeps only the still-visible ids counted as selected after narrowing the filter', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Broken A', error_count: 3 }),
      makeSub({ feed_id: 2, title: 'Broken B', error_count: 3 }),
    ]
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /⚠ エラーのみ/ }))

    // Select all (both rows) via the header checkbox.
    fireEvent.click(screen.getByRole('checkbox', { name: '全選択' }))
    expect(screen.getByText(/選択中/)).toHaveTextContent('2 件選択中')

    // Narrowing the query to just "Broken A" should drop the now-hidden
    // row's selection out of the visible count, even though it's still
    // tracked internally (selectedInView vs. selected -- see
    // FeedManagerPane's own comment on why bulk unsubscribe stays scoped
    // to the currently filtered set).
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'Broken A' },
    })
    expect(screen.getByText(/選択中/)).toHaveTextContent('1 件選択中')
    expect(screen.getByRole('checkbox', { name: '全選択' })).toBeChecked()
  })

  it('unchecking select-all only clears the currently filtered rows', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Broken A', error_count: 3 }),
      makeSub({ feed_id: 2, title: 'Broken B', error_count: 3 }),
    ]
    render(<FeedManagerPane />)
    fireEvent.click(screen.getByRole('button', { name: /⚠ エラーのみ/ }))
    fireEvent.click(screen.getByRole('checkbox', { name: '全選択' }))

    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: 'Broken A' },
    })
    fireEvent.click(screen.getByRole('checkbox', { name: '全選択' }))
    expect(screen.getByText(/選択中/)).toHaveTextContent('0 件選択中')

    // Broken B was never in view while unchecking, so it should still be
    // selected once the filter is cleared again.
    fireEvent.input(screen.getByPlaceholderText('タイトル・URLで絞り込み'), {
      target: { value: '' },
    })
    expect(screen.getByText(/選択中/)).toHaveTextContent('1 件選択中')
  })
})
