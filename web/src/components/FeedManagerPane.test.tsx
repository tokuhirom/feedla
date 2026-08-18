// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { SubscriptionView } from '../api/types'
import { folders, subscriptions } from '../state/subscriptions'
import { feedManagerInitialOnlyErrors } from '../state/ui'
import { FeedManagerPane } from './FeedManagerPane'

function makeSub(overrides: Partial<SubscriptionView> = {}): SubscriptionView {
  return {
    feed_id: 1,
    feed_url: 'https://example.com/feed.xml',
    title: 'Example Feed',
    rating: 0,
    unread_count: 0,
    error_count: 0,
    next_fetch_at: 0,
    kind: 'rss',
    ...overrides,
  } as SubscriptionView
}

beforeEach(() => {
  subscriptions.value = []
  folders.value = []
  feedManagerInitialOnlyErrors.value = false
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
