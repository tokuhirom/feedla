// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SubscriptionView } from '../api/types'
import * as actions from '../state/actions'
import { folders, selectedFeedId, subscriptions } from '../state/subscriptions'
import { feedDetailOpen } from '../state/ui'
import { FeedDetailOverlay } from './FeedDetailOverlay'

vi.mock('../state/actions', async (importOriginal) => ({
  ...(await importOriginal<typeof actions>()),
  selectAndLoadFeed: vi.fn(),
}))

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
  selectedFeedId.value = null
  feedDetailOpen.value = false
})

afterEach(() => {
  cleanup()
})

describe('FeedDetailOverlay same-site hint', () => {
  it('shows no hint when no other feed shares the site_url', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, site_url: 'https://example.com/' }),
    ]
    selectedFeedId.value = 1
    feedDetailOpen.value = true
    render(<FeedDetailOverlay />)
    expect(screen.queryByText(/同じサイト URL/)).not.toBeInTheDocument()
  })

  it('lists other feeds sharing the same site_url', () => {
    subscriptions.value = [
      makeSub({
        feed_id: 1,
        title: 'Feed A',
        site_url: 'https://example.com/',
      }),
      makeSub({
        feed_id: 2,
        title: 'Feed B',
        feed_url: 'https://example.com/feed2.xml',
        site_url: 'https://example.com/',
      }),
    ]
    selectedFeedId.value = 1
    feedDetailOpen.value = true
    render(<FeedDetailOverlay />)
    expect(screen.getByText(/同じサイト URL/)).toBeInTheDocument()
    expect(screen.getByText('Feed B')).toBeInTheDocument()
  })

  it('switches to the clicked same-site feed via selectAndLoadFeed', () => {
    subscriptions.value = [
      makeSub({
        feed_id: 1,
        title: 'Feed A',
        site_url: 'https://example.com/',
      }),
      makeSub({
        feed_id: 2,
        title: 'Feed B',
        feed_url: 'https://example.com/feed2.xml',
        site_url: 'https://example.com/',
      }),
    ]
    selectedFeedId.value = 1
    feedDetailOpen.value = true
    render(<FeedDetailOverlay />)
    fireEvent.click(screen.getByText('Feed B'))
    expect(actions.selectAndLoadFeed).toHaveBeenCalledWith(2)
  })

  it('does not treat feeds with no site_url as matching each other', () => {
    subscriptions.value = [
      makeSub({ feed_id: 1, title: 'Feed A' }),
      makeSub({ feed_id: 2, title: 'Feed B', feed_url: 'https://x.example/f' }),
    ]
    selectedFeedId.value = 1
    feedDetailOpen.value = true
    render(<FeedDetailOverlay />)
    expect(screen.queryByText(/同じサイト URL/)).not.toBeInTheDocument()
  })
})
