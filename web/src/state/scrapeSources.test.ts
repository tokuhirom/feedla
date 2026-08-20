import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../api/client'
import type { ScrapeSource } from '../api/types'
import {
  addIgnorePatternRaw,
  ignoreBlockText,
  loadScrapeSources,
  removeIgnorePattern,
  scrapeSourceForFeed,
  scrapeSources,
  setWatchMode,
} from './scrapeSources'
import { toast } from './ui'

vi.mock('../api/client', () => ({
  listScrapeSources: vi.fn(),
  patchScrapeSourceConfig: vi.fn(),
}))

function makeSource(overrides: Partial<ScrapeSource> = {}): ScrapeSource {
  return {
    id: 1,
    feed_id: 10,
    kind: 'pagewatch',
    target_url: 'https://example.com',
    config: {},
    created_by: 1,
    created_at: 0,
    updated_at: 0,
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  scrapeSources.value = []
  toast.value = null
})

describe('loadScrapeSources', () => {
  it('fetches and stores the list', async () => {
    const sources = [makeSource()]
    vi.mocked(api.listScrapeSources).mockResolvedValue({
      scrape_sources: sources,
    })
    await loadScrapeSources()
    expect(scrapeSources.value).toEqual(sources)
  })
})

describe('scrapeSourceForFeed', () => {
  it('finds the source matching the feed id', () => {
    const source = makeSource({ feed_id: 42 })
    scrapeSources.value = [makeSource({ id: 2, feed_id: 1 }), source]
    expect(scrapeSourceForFeed(42)).toBe(source)
  })

  it('returns undefined when no source matches', () => {
    scrapeSources.value = [makeSource({ feed_id: 1 })]
    expect(scrapeSourceForFeed(99)).toBeUndefined()
  })
})

describe('setWatchMode', () => {
  it('merges the new watch_mode into the existing config and patches', async () => {
    const source = makeSource({
      config: { watch_mode: 'additions', min_change_chars: 5 },
    })
    scrapeSources.value = [source]
    const updated = {
      ...source,
      config: { ...source.config, watch_mode: 'changes' as const },
    }
    vi.mocked(api.patchScrapeSourceConfig).mockResolvedValue(updated)

    await setWatchMode(10, 'changes')

    expect(api.patchScrapeSourceConfig).toHaveBeenCalledWith(1, {
      watch_mode: 'changes',
      min_change_chars: 5,
    })
    expect(scrapeSources.value).toEqual([updated])
  })

  it('shows a toast and does not call the API when the feed has no source', async () => {
    scrapeSources.value = []
    await setWatchMode(999, 'changes')
    expect(api.patchScrapeSourceConfig).not.toHaveBeenCalled()
    expect(toast.value).toEqual({
      message: '監視設定が見つかりません',
      variant: 'info',
    })
  })

  it('shows a toast when the API call fails', async () => {
    scrapeSources.value = [makeSource()]
    vi.mocked(api.patchScrapeSourceConfig).mockRejectedValue(new Error('boom'))
    await setWatchMode(10, 'changes')
    expect(toast.value).toEqual({ message: 'boom', variant: 'info' })
  })
})

describe('removeIgnorePattern', () => {
  it('filters the pattern out of the existing list', async () => {
    const source = makeSource({
      config: { ignore_patterns: ['foo', 'bar'] },
    })
    scrapeSources.value = [source]
    vi.mocked(api.patchScrapeSourceConfig).mockResolvedValue(source)

    await removeIgnorePattern(10, 'foo')

    expect(api.patchScrapeSourceConfig).toHaveBeenCalledWith(1, {
      ignore_patterns: ['bar'],
    })
  })

  it('tolerates a missing ignore_patterns field', async () => {
    const source = makeSource({ config: {} })
    scrapeSources.value = [source]
    vi.mocked(api.patchScrapeSourceConfig).mockResolvedValue(source)

    await removeIgnorePattern(10, 'foo')

    expect(api.patchScrapeSourceConfig).toHaveBeenCalledWith(1, {
      ignore_patterns: [],
    })
  })
})

describe('addIgnorePatternRaw', () => {
  it('appends the pattern to the existing list', async () => {
    const source = makeSource({ config: { ignore_patterns: ['foo'] } })
    scrapeSources.value = [source]
    vi.mocked(api.patchScrapeSourceConfig).mockResolvedValue(source)

    await addIgnorePatternRaw(10, 'bar')

    expect(api.patchScrapeSourceConfig).toHaveBeenCalledWith(1, {
      ignore_patterns: ['foo', 'bar'],
    })
  })

  it('shows a toast and skips the API call for a duplicate pattern', async () => {
    scrapeSources.value = [makeSource({ config: { ignore_patterns: ['foo'] } })]

    await addIgnorePatternRaw(10, 'foo')

    expect(api.patchScrapeSourceConfig).not.toHaveBeenCalled()
    expect(toast.value).toEqual({
      message: 'すでに無視パターンに登録されています',
      variant: 'info',
    })
  })
})

describe('ignoreBlockText', () => {
  it('derives a pattern from the block text and adds it', async () => {
    const source = makeSource({ config: {} })
    scrapeSources.value = [source]
    vi.mocked(api.patchScrapeSourceConfig).mockResolvedValue(source)

    await ignoreBlockText(10, '閲覧数: 123')

    expect(api.patchScrapeSourceConfig).toHaveBeenCalledWith(1, {
      ignore_patterns: ['閲覧数: \\d+'],
    })
    expect(toast.value).toEqual({
      message: 'このブロックを無視するように設定しました',
      variant: 'info',
    })
  })

  it('does nothing when the derived pattern is empty', async () => {
    scrapeSources.value = [makeSource({ config: {} })]

    await ignoreBlockText(10, '   ')

    expect(api.patchScrapeSourceConfig).not.toHaveBeenCalled()
    expect(toast.value).toBeNull()
  })
})
