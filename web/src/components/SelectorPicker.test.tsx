// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../api/client'
import type { InspectResult } from '../api/types'
import { SelectorPicker } from './SelectorPicker'

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return { ...actual, inspectScrapeSource: vi.fn() }
})

// Mirrors a <ul id="news-list"><li class="post">...</li>...</ul> listing --
// the same shape lib/selectorGen.test.ts exercises directly.
const inspectResult: InspectResult = {
  view_url: 'https://api.example/api/v1/scrape_sources/inspect/view?t=tok',
  expires_at: 9999999999,
  elements: [
    { id: 1, tag: 'ul', html_id: 'news-list', parent_id: 0 },
    { id: 2, tag: 'li', classes: ['post'], parent_id: 1 },
    { id: 3, tag: 'h3', classes: ['title'], parent_id: 2 },
    { id: 4, tag: 'li', classes: ['post'], parent_id: 1 },
    { id: 5, tag: 'h3', classes: ['title'], parent_id: 4 },
    { id: 6, tag: 'li', classes: ['post'], parent_id: 1 },
    { id: 7, tag: 'h3', classes: ['title'], parent_id: 6 },
  ],
}

function postFromIframe(iframe: HTMLIFrameElement, data: unknown): void {
  window.dispatchEvent(
    new MessageEvent('message', {
      data,
      source: iframe.contentWindow,
      origin: 'null',
    }),
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.inspectScrapeSource).mockResolvedValue(inspectResult)
})

afterEach(() => {
  cleanup()
})

describe('SelectorPicker', () => {
  it('requests an inspect session for the given url and renders a sandboxed iframe with no allow-same-origin', async () => {
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    expect(api.inspectScrapeSource).toHaveBeenCalledWith(
      'https://target.example/news',
    )
    const iframe = await screen.findByTitle('ページのプレビュー')
    expect(iframe).toHaveAttribute('src', inspectResult.view_url)
    expect(iframe).toHaveAttribute('sandbox', 'allow-scripts')
    expect(iframe.getAttribute('sandbox')).not.toContain('allow-same-origin')
  })

  it('turns a valid click message into item_selector candidates', async () => {
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    const iframe =
      await screen.findByTitle<HTMLIFrameElement>('ページのプレビュー')

    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 6 })

    await screen.findByText('#news-list > li.post')
    expect(screen.getByText('マッチ数: 3')).toBeInTheDocument()
  })

  it('posts the candidate id groups to the iframe as a highlight message', async () => {
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    const iframe =
      await screen.findByTitle<HTMLIFrameElement>('ページのプレビュー')
    const post = vi.spyOn(iframe.contentWindow!, 'postMessage')

    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 6 })
    await screen.findByText('#news-list > li.post')

    await waitFor(() => {
      expect(post).toHaveBeenCalledWith(
        { type: 'feedla-inspect-highlight', groups: [[2, 4, 6]] },
        '*',
      )
    })

    // Choosing the item keeps its matches framed (single group, color 0).
    post.mockClear()
    screen.getByRole('button', { name: 'これを使う' }).click()
    await screen.findByRole('button', { name: 'タイトルは指定せず確定' })
    await waitFor(() => {
      expect(post).toHaveBeenCalledWith(
        { type: 'feedla-inspect-highlight', groups: [[2, 4, 6]] },
        '*',
      )
    })
  })

  it("ignores a message whose source is not this component's own iframe", async () => {
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    await screen.findByTitle('ページのプレビュー')

    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'feedla-inspect-click', id: 6 },
        source: window, // not the iframe's contentWindow
        origin: 'null',
      }),
    )

    await new Promise((r) => setTimeout(r, 0))
    expect(screen.queryByText('#news-list > li.post')).not.toBeInTheDocument()
  })

  it('ignores a click id that is not in the server-supplied element index', async () => {
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    const iframe =
      await screen.findByTitle<HTMLIFrameElement>('ページのプレビュー')

    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 9999 })

    await new Promise((r) => setTimeout(r, 0))
    expect(
      screen.queryByText(/候補を生成できませんでした/),
    ).not.toBeInTheDocument()
  })

  it('applies the chosen item_selector without a title_selector via "タイトルは指定せず確定"', async () => {
    const onApply = vi.fn()
    const onClose = vi.fn()
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={onApply}
        onClose={onClose}
      />,
    )
    const iframe =
      await screen.findByTitle<HTMLIFrameElement>('ページのプレビュー')
    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 6 })
    await screen.findByText('#news-list > li.post')

    screen.getByRole('button', { name: 'これを使う' }).click()
    await screen.findByRole('button', { name: 'タイトルは指定せず確定' })
    screen.getByRole('button', { name: 'タイトルは指定せず確定' }).click()

    expect(onApply).toHaveBeenCalledWith({
      itemSelector: '#news-list > li.post',
      titleSelector: undefined,
    })
    expect(onClose).toHaveBeenCalled()
  })

  it('generates a title_selector from a second click inside the chosen item', async () => {
    const onApply = vi.fn()
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={onApply}
        onClose={vi.fn()}
      />,
    )
    const iframe =
      await screen.findByTitle<HTMLIFrameElement>('ページのプレビュー')
    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 6 })
    await screen.findByText('#news-list > li.post')
    screen.getByRole('button', { name: 'これを使う' }).click()
    await screen.findByRole('button', { name: 'タイトルは指定せず確定' })

    // id 7 is the <h3 class="title"> inside item id 6.
    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 7 })
    await screen.findByText('h3.title')

    screen.getByRole('button', { name: 'この設定を使う' }).click()
    await waitFor(() => {
      expect(onApply).toHaveBeenCalledWith({
        itemSelector: '#news-list > li.post',
        titleSelector: 'h3.title',
      })
    })
  })

  it('hints instead of generating a title_selector for a click outside the chosen item', async () => {
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    const iframe =
      await screen.findByTitle<HTMLIFrameElement>('ページのプレビュー')
    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 6 })
    await screen.findByText('#news-list > li.post')
    screen.getByRole('button', { name: 'これを使う' }).click()
    await screen.findByRole('button', { name: 'タイトルは指定せず確定' })

    // id 1 (<ul>) is outside every matched <li> -- the title step accepts a
    // click inside *any* matched item (not just the one originally
    // clicked to build item_selector), so this has to be truly outside
    // the whole list to trigger the hint.
    postFromIframe(iframe, { type: 'feedla-inspect-click', id: 1 })

    await screen.findByText(/選択した記事の範囲外です/)
  })

  it('calls onClose from the キャンセル button', async () => {
    const onClose = vi.fn()
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={onClose}
      />,
    )
    await screen.findByTitle('ページのプレビュー')
    screen.getByRole('button', { name: 'キャンセル' }).click()
    expect(onClose).toHaveBeenCalled()
  })

  it('shows a retryable error when the inspect request fails', async () => {
    vi.mocked(api.inspectScrapeSource).mockRejectedValue(new Error('boom'))
    render(
      <SelectorPicker
        url="https://target.example/news"
        onApply={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    await screen.findByText('boom')
    expect(
      screen.getByRole('button', { name: '再読み込み' }),
    ).toBeInTheDocument()
  })
})
