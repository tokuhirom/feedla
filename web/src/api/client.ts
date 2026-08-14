import type { Candidate, Entry, Folder, SubscriptionView } from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new ApiError(res.status, text || res.statusText)
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

export function listSubscriptions(): Promise<{ subscriptions: SubscriptionView[] }> {
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
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new ApiError(res.status, text || res.statusText)
  }
  const body = await res.json()
  if (res.status === 202) {
    return { status: 'candidates', candidates: body.candidates as Candidate[] }
  }
  return { status: 'created', subscription: body.subscription as SubscriptionView }
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
  return apiFetch(`/api/v1/subscriptions/${feedId}/entries${qs ? `?${qs}` : ''}`)
}

export function readAll(feedId: number, before: number): Promise<{ marked_read: number }> {
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

export function markEntriesRead(entryIds: number[]): Promise<{ marked_read: number }> {
  return apiFetch('/api/v1/entries/read', {
    method: 'POST',
    body: JSON.stringify({ entry_ids: entryIds }),
  })
}
