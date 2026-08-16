import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { PagewatchConfig, ScrapeSource } from '../api/types'
import { buildIgnorePattern } from '../utils/ignorePattern'
import { showToast } from './ui'

// One entry per pagewatch subscription (id/feed_id/config only -- state is
// intentionally not exposed by the API, see api/types.ts's ScrapeSource
// comment). Loaded once at startup alongside subscriptions, the same way
// ignoreWords.ts is: there's normally only a handful of these, so a single
// list fetched up front is simpler than fetching per-feed on demand.
export const scrapeSources = signal<ScrapeSource[]>([])

export async function loadScrapeSources(): Promise<void> {
  const res = await api.listScrapeSources()
  scrapeSources.value = res.scrape_sources
}

export function scrapeSourceForFeed(feedId: number): ScrapeSource | undefined {
  return scrapeSources.value.find((s) => s.feed_id === feedId)
}

function applyScrapeSourcePatch(updated: ScrapeSource): void {
  scrapeSources.value = scrapeSources.value.map((s) =>
    s.id === updated.id ? updated : s,
  )
}

/** PATCH replaces the whole config server-side (see
 * UpdateScrapeSourceConfig), so every caller here reads the currently-known
 * config, applies `updater`, and sends the merged result back -- never a
 * bare `{ignore_patterns: [...]}` that would silently drop watch_mode etc. */
async function patchConfig(
  feedId: number,
  updater: (cfg: PagewatchConfig) => PagewatchConfig,
): Promise<void> {
  const source = scrapeSourceForFeed(feedId)
  if (!source) {
    showToast('監視設定が見つかりません')
    return
  }
  try {
    const updated = await api.patchScrapeSourceConfig(
      source.id,
      updater(source.config),
    )
    applyScrapeSourcePatch(updated)
  } catch (e) {
    showToast(e instanceof Error ? e.message : String(e))
  }
}

export function setWatchMode(
  feedId: number,
  watchMode: 'additions' | 'changes',
): Promise<void> {
  return patchConfig(feedId, (cfg) => ({ ...cfg, watch_mode: watchMode }))
}

export function removeIgnorePattern(
  feedId: number,
  pattern: string,
): Promise<void> {
  return patchConfig(feedId, (cfg) => ({
    ...cfg,
    ignore_patterns: (cfg.ignore_patterns ?? []).filter((p) => p !== pattern),
  }))
}

async function addIgnorePattern(
  feedId: number,
  pattern: string,
): Promise<void> {
  const source = scrapeSourceForFeed(feedId)
  const current = source?.config.ignore_patterns ?? []
  if (current.includes(pattern)) {
    showToast('すでに無視パターンに登録されています')
    return
  }
  await patchConfig(feedId, (cfg) => ({
    ...cfg,
    ignore_patterns: [...(cfg.ignore_patterns ?? []), pattern],
  }))
}

/** Manual entry from FeedDetailOverlay's 監視設定 section -- the pattern is
 * already a regexp the user typed themselves. */
export function addIgnorePatternRaw(
  feedId: number,
  pattern: string,
): Promise<void> {
  return addIgnorePattern(feedId, pattern)
}

/** The §9.4 "このブロックを無視する" recovery button on a diff block in
 * EntryItem: derives the regexp from the block's own text instead of
 * making the reader write one. */
export async function ignoreBlockText(
  feedId: number,
  blockText: string,
): Promise<void> {
  const pattern = buildIgnorePattern(blockText)
  if (!pattern) return
  await addIgnorePattern(feedId, pattern)
  showToast('このブロックを無視するように設定しました')
}
