import { showErrorToast } from '../state/ui'
import type {
  Candidate,
  Entry,
  Folder,
  IgnoreWord,
  PagewatchConfig,
  Pin,
  PreviewBlock,
  ScrapeSource,
  Stats,
  SubscriptionView,
} from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

/** Throws ApiError for any non-ok response. 5xx also raises a visible error
 * toast here, at the one place every request passes through -- without
 * this, a background request (e.g. the debounced mark-as-read flush in
 * state/entries.ts, which only retries silently) can keep failing with no
 * on-screen sign anything is wrong. 4xx is left to callers, since those are
 * typically expected/validation failures with their own specific message
 * already surfaced locally (see state/actions.ts). */
async function throwIfNotOk(res: Response): Promise<void> {
  if (res.ok) return
  const text = await res.text().catch(() => '')
  if (res.status >= 500) {
    showErrorToast(`サーバーエラーが発生しました (${res.status})`)
  }
  throw new ApiError(res.status, text || res.statusText)
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  await throwIfNotOk(res)
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

export function listSubscriptions(): Promise<{
  subscriptions: SubscriptionView[]
  today_unread_count: number
}> {
  return apiFetch('/api/v1/subscriptions')
}

export function listFolders(): Promise<{ folders: Folder[] }> {
  return apiFetch('/api/v1/folders')
}

export function createFolder(name: string): Promise<Folder> {
  return apiFetch('/api/v1/folders', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
}

export type CreateSubscriptionResult =
  | { status: 'created'; subscription: SubscriptionView }
  | { status: 'candidates'; candidates: Candidate[] }

export async function createSubscription(req: {
  url: string
  folder_id?: number
  title?: string
}): Promise<CreateSubscriptionResult> {
  const res = await fetch('/api/v1/subscriptions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  await throwIfNotOk(res)
  const body = await res.json()
  if (res.status === 202) {
    return { status: 'candidates', candidates: body.candidates as Candidate[] }
  }
  return {
    status: 'created',
    subscription: body.subscription as SubscriptionView,
  }
}

export function patchSubscription(
  feedId: number,
  patch: { title?: string; rating?: number; folder_id?: number | null },
): Promise<SubscriptionView> {
  return apiFetch(`/api/v1/subscriptions/${feedId}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  })
}

export function deleteSubscription(feedId: number): Promise<void> {
  return apiFetch(`/api/v1/subscriptions/${feedId}`, { method: 'DELETE' })
}

export function listEntries(
  feedId: number,
  opts: { unread?: boolean; limit?: number; cursor?: string } = {},
): Promise<{ entries: Entry[]; next_cursor?: string }> {
  const params = new URLSearchParams()
  if (opts.unread) params.set('unread', '1')
  if (opts.limit) params.set('limit', String(opts.limit))
  if (opts.cursor) params.set('cursor', opts.cursor)
  const qs = params.toString()
  return apiFetch(
    `/api/v1/subscriptions/${feedId}/entries${qs ? `?${qs}` : ''}`,
  )
}

export type GroupEntriesFilter =
  | { folderId: number | null }
  | { rating: number }

// Backs "read everything in this folder/priority level at once" -- the
// sidebar's group headers link here instead of a single subscription.
export function listGroupEntries(
  filter: GroupEntriesFilter,
  opts: { unread?: boolean; limit?: number; cursor?: string } = {},
): Promise<{ entries: Entry[]; next_cursor?: string }> {
  const params = new URLSearchParams()
  if ('folderId' in filter) {
    params.set('folder_id', String(filter.folderId ?? 0))
  } else {
    params.set('rating', String(filter.rating))
  }
  if (opts.unread) params.set('unread', '1')
  if (opts.limit) params.set('limit', String(opts.limit))
  if (opts.cursor) params.set('cursor', opts.cursor)
  return apiFetch(`/api/v1/entries?${params.toString()}`)
}

// Backs the sidebar's pinned "Today" group -- every unread entry published
// in the last 24 hours across every feed, regardless of rating.
export function listTodayEntries(
  opts: { limit?: number; cursor?: string } = {},
): Promise<{ entries: Entry[]; next_cursor?: string }> {
  const params = new URLSearchParams()
  if (opts.limit) params.set('limit', String(opts.limit))
  if (opts.cursor) params.set('cursor', opts.cursor)
  const qs = params.toString()
  return apiFetch(`/api/v1/entries/today${qs ? `?${qs}` : ''}`)
}

export function readAll(
  feedId: number,
  before: number,
): Promise<{ marked_read: number }> {
  return apiFetch(`/api/v1/subscriptions/${feedId}/read_all`, {
    method: 'POST',
    body: JSON.stringify({ before }),
  })
}

export function refreshSubscription(
  feedId: number,
): Promise<{ new_entries: number; error?: string }> {
  return apiFetch(`/api/v1/subscriptions/${feedId}/refresh`, { method: 'POST' })
}

export function markEntriesRead(
  entryIds: number[],
): Promise<{ marked_read: number }> {
  return apiFetch('/api/v1/entries/read', {
    method: 'POST',
    body: JSON.stringify({ entry_ids: entryIds }),
    // The debounced flush in state/entries.ts clears its pending-id set
    // before this resolves; without keepalive, a page reload/close mid
    // request aborts the fetch (server sees "context canceled") and the
    // read state is lost even though flushOnUnload's sendBeacon fallback
    // has nothing left to resend.
    keepalive: true,
  })
}

export function markAllEntriesRead(): Promise<{ marked_read: number }> {
  return apiFetch('/api/v1/entries/read_all', { method: 'POST' })
}

export function searchEntries(
  query: string,
  opts: { limit?: number; cursor?: string } = {},
): Promise<{ entries: Entry[]; next_cursor?: string }> {
  const params = new URLSearchParams({ q: query })
  if (opts.limit) params.set('limit', String(opts.limit))
  if (opts.cursor) params.set('cursor', opts.cursor)
  return apiFetch(`/api/v1/search?${params.toString()}`)
}

export function listPins(): Promise<{ pins: Pin[] }> {
  return apiFetch('/api/v1/pins')
}

export function addPin(entryId: number): Promise<{ entry_id: number }> {
  return apiFetch('/api/v1/pins', {
    method: 'POST',
    body: JSON.stringify({ entry_id: entryId }),
  })
}

export function removePin(entryId: number): Promise<void> {
  return apiFetch(`/api/v1/pins/${entryId}`, { method: 'DELETE' })
}

export function listIgnoreWords(): Promise<{ ignore_words: IgnoreWord[] }> {
  return apiFetch('/api/v1/ignore_words')
}

export function addIgnoreWord(word: string): Promise<{ word: string }> {
  return apiFetch('/api/v1/ignore_words', {
    method: 'POST',
    body: JSON.stringify({ word }),
  })
}

export function removeIgnoreWord(id: number): Promise<void> {
  return apiFetch(`/api/v1/ignore_words/${id}`, { method: 'DELETE' })
}

export function getStats(): Promise<Stats> {
  return apiFetch('/api/v1/stats')
}

// Registers a page-watch subscription (POST /api/v1/scrape_sources) --
// the "フィードが見つからないのでページの更新を監視する" fallback offered by
// AddSubscriptionDialog when createSubscription 502s. Unlike
// createSubscription this never returns a candidate list: the caller has
// already picked a single URL to watch.
export async function createScrapeSource(req: {
  url: string
  folder_id?: number
  title?: string
}): Promise<SubscriptionView> {
  const res = await apiFetch<{ subscription: SubscriptionView }>(
    '/api/v1/scrape_sources',
    { method: 'POST', body: JSON.stringify(req) },
  )
  return res.subscription
}

export function listScrapeSources(): Promise<{
  scrape_sources: ScrapeSource[]
}> {
  return apiFetch('/api/v1/scrape_sources')
}

export function patchScrapeSourceConfig(
  id: number,
  config: PagewatchConfig,
): Promise<ScrapeSource> {
  return apiFetch(`/api/v1/scrape_sources/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ config }),
  })
}

// Fetches the target page right now and returns the blocks pagewatch would
// extract under the source's currently-saved config -- no side effects, no
// diffing (see handlePreviewScrapeSource on the Go side).
export function previewScrapeSource(
  id: number,
): Promise<{ blocks: PreviewBlock[] }> {
  return apiFetch(`/api/v1/scrape_sources/${id}/preview`, { method: 'POST' })
}

export async function importOpml(file: File): Promise<{ imported: number }> {
  const res = await fetch('/api/v1/opml', {
    method: 'POST',
    headers: { 'Content-Type': 'text/x-opml' },
    body: file,
  })
  await throwIfNotOk(res)
  return (await res.json()) as { imported: number }
}
