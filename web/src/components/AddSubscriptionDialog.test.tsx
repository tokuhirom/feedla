// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../api/client'
import { addDialogOpen } from '../state/ui'
import { AddSubscriptionDialog } from './AddSubscriptionDialog'

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    createSubscription: vi.fn(),
    createScrapeSource: vi.fn(),
  }
})

vi.mock('../state/entries', () => ({
  loadEntries: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../state/scrapeSources', () => ({
  loadScrapeSources: vi.fn().mockResolvedValue(undefined),
}))

beforeEach(() => {
  vi.clearAllMocks()
  addDialogOpen.value = true
})

afterEach(() => {
  cleanup()
})

describe('AddSubscriptionDialog visibility', () => {
  it('renders nothing when closed', () => {
    addDialogOpen.value = false
    const { container } = render(<AddSubscriptionDialog />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('AddSubscriptionDialog submit form', () => {
  it('disables submit until a URL is entered', () => {
    render(<AddSubscriptionDialog />)
    const submit = screen.getByRole('button', { name: '追加' })
    expect(submit).toBeDisabled()
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: '  ' },
      },
    )
    expect(submit).toBeDisabled()
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com/feed.xml' },
      },
    )
    expect(submit).not.toBeDisabled()
  })

  it('always calls createSubscription unconfirmed on the initial submit', () => {
    vi.mocked(api.createSubscription).mockResolvedValue({
      status: 'candidates',
      candidates: [
        {
          title: 'Feed A',
          feed_url: 'https://a.example/feed',
          fulltext: false,
        },
        { title: 'Feed A', feed_url: 'https://a.example/feed', fulltext: true },
      ],
    })
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com/feed.xml' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))

    expect(api.createSubscription).toHaveBeenCalledWith({
      url: 'https://example.com/feed.xml',
      confirmed: undefined,
      fulltext: undefined,
      title: undefined,
    })
  })
})

describe('AddSubscriptionDialog candidate selection', () => {
  it('shows a candidate list -- even for a single discovered feed, since the fulltext variant is always appended', async () => {
    vi.mocked(api.createSubscription).mockResolvedValue({
      status: 'candidates',
      candidates: [
        {
          title: 'Feed A',
          feed_url: 'https://a.example/feed',
          fulltext: false,
        },
        { title: 'Feed A', feed_url: 'https://a.example/feed', fulltext: true },
      ],
    })
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))

    await screen.findByText('購読方法を選択してください:')
    expect(screen.getByRole('button', { name: 'Feed A' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Feed A (本文抽出あり)' }),
    ).toBeInTheDocument()
  })

  it('falls back to the feed_url when a candidate has no title', async () => {
    vi.mocked(api.createSubscription).mockResolvedValue({
      status: 'candidates',
      candidates: [
        { title: '', feed_url: 'https://b.example/feed', fulltext: false },
        { title: '', feed_url: 'https://b.example/feed', fulltext: true },
      ],
    })
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))

    await screen.findByRole('button', { name: 'https://b.example/feed' })
    expect(
      screen.getByRole('button', {
        name: 'https://b.example/feed (本文抽出あり)',
      }),
    ).toBeInTheDocument()
  })

  it('subscribes to the chosen plain candidate, confirmed and without fulltext', async () => {
    vi.mocked(api.createSubscription)
      .mockResolvedValueOnce({
        status: 'candidates',
        candidates: [
          {
            title: 'Feed A',
            feed_url: 'https://a.example/feed',
            fulltext: false,
          },
          {
            title: 'Feed A',
            feed_url: 'https://a.example/feed',
            fulltext: true,
          },
        ],
      })
      .mockResolvedValueOnce({
        status: 'created',
        subscription: {
          feed_id: 1,
          feed_url: 'a',
          title: 'Feed A',
          rating: 0,
        } as never,
      })
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))
    await screen.findByRole('button', { name: 'Feed A' })

    fireEvent.click(screen.getByRole('button', { name: 'Feed A' }))

    await vi.waitFor(() => {
      expect(addDialogOpen.value).toBe(false)
    })
    expect(api.createSubscription).toHaveBeenLastCalledWith({
      url: 'https://a.example/feed',
      confirmed: true,
      fulltext: false,
      title: 'Feed A',
    })
  })

  it('subscribes to the fulltext candidate with fulltext: true', async () => {
    vi.mocked(api.createSubscription)
      .mockResolvedValueOnce({
        status: 'candidates',
        candidates: [
          {
            title: 'Feed A',
            feed_url: 'https://a.example/feed',
            fulltext: false,
          },
          {
            title: 'Feed A',
            feed_url: 'https://a.example/feed',
            fulltext: true,
          },
        ],
      })
      .mockResolvedValueOnce({
        status: 'created',
        subscription: {
          feed_id: 1,
          feed_url: 'a',
          title: 'Feed A',
          rating: 0,
        } as never,
      })
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))
    await screen.findByRole('button', { name: 'Feed A (本文抽出あり)' })

    fireEvent.click(
      screen.getByRole('button', { name: 'Feed A (本文抽出あり)' }),
    )

    await vi.waitFor(() => {
      expect(addDialogOpen.value).toBe(false)
    })
    expect(api.createSubscription).toHaveBeenLastCalledWith({
      url: 'https://a.example/feed',
      confirmed: true,
      fulltext: true,
      title: 'Feed A',
    })
  })

  it('returns to the form from the candidate list via 戻る', async () => {
    vi.mocked(api.createSubscription).mockResolvedValue({
      status: 'candidates',
      candidates: [
        {
          title: 'Feed A',
          feed_url: 'https://a.example/feed',
          fulltext: false,
        },
        { title: 'Feed A', feed_url: 'https://a.example/feed', fulltext: true },
      ],
    })
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))
    await screen.findByRole('button', { name: '戻る' })

    fireEvent.click(screen.getByRole('button', { name: '戻る' }))

    expect(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
    ).toBeInTheDocument()
  })
})

describe('AddSubscriptionDialog pagewatch fallback', () => {
  it('shows a plain error without the pagewatch offer for a non-502 failure', async () => {
    vi.mocked(api.createSubscription).mockRejectedValue(new Error('boom'))
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))

    await screen.findByText('boom')
    expect(
      screen.queryByText('このページにフィードが見つかりませんでした。'),
    ).not.toBeInTheDocument()
  })

  it('offers the pagewatch fallback and hides the raw error on a 502', async () => {
    vi.mocked(api.createSubscription).mockRejectedValue(
      new api.ApiError(502, 'no feed found'),
    )
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))

    await screen.findByText('このページにフィードが見つかりませんでした。')
    expect(screen.queryByText('no feed found')).not.toBeInTheDocument()
  })

  it('registers a page watch and closes the dialog on success', async () => {
    vi.mocked(api.createSubscription).mockRejectedValue(
      new api.ApiError(502, 'no feed found'),
    )
    vi.mocked(api.createScrapeSource).mockResolvedValue({
      feed_id: 1,
      feed_url: 'https://example.com',
      title: 'Example',
      rating: 0,
    } as never)
    render(<AddSubscriptionDialog />)
    fireEvent.input(
      screen.getByPlaceholderText('https://example.com/feed.xml'),
      {
        target: { value: 'https://example.com' },
      },
    )
    fireEvent.click(screen.getByRole('button', { name: '追加' }))
    await screen.findByRole('button', { name: 'ページの更新を監視する' })

    fireEvent.click(
      screen.getByRole('button', { name: 'ページの更新を監視する' }),
    )

    await vi.waitFor(() => {
      expect(addDialogOpen.value).toBe(false)
    })
    expect(api.createScrapeSource).toHaveBeenCalledWith({
      url: 'https://example.com',
    })
  })
})
