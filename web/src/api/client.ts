import { showErrorToast } from '../state/ui'
import type {
  AdminBackupStatus,
  AdminUser,
  Candidate,
  Entry,
  Folder,
  HealthStatus,
  IgnoreWord,
  Invitation,
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
 * already surfaced locally (see state/actions.ts).
 *
 * 401 additionally dispatches a window event instead of importing
 * state/auth.ts directly (which would create an import cycle: state/auth.ts
 * calls into this module for login/logout/getMe) -- state/auth.ts listens
 * for it to drop back to the login screen when a session expires mid-use. */
async function throwIfNotOk(res: Response): Promise<void> {
  if (res.ok) return
  const text = await res.text().catch(() => '')
  if (res.status === 401) {
    window.dispatchEvent(new Event('feedla:unauthorized'))
  } else if (res.status >= 500) {
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
  // confirmed/fulltext: see internal/api's createSubscriptionRequest.
  // confirmed skips discovery and subscribes to url directly (the caller
  // already resolved it from a prior candidates response); fulltext
  // enables internal/fulltext extraction before the first crawl.
  confirmed?: boolean
  fulltext?: boolean
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

// enableFulltext/disableFulltext toggle internal/fulltext extraction for an
// existing subscription (unrelated to createScrapeSource/pagewatch below).
export function enableFulltext(feedId: number): Promise<SubscriptionView> {
  return apiFetch(`/api/v1/subscriptions/${feedId}/fulltext`, {
    method: 'POST',
  })
}

export function disableFulltext(feedId: number): Promise<SubscriptionView> {
  return apiFetch(`/api/v1/subscriptions/${feedId}/fulltext`, {
    method: 'DELETE',
  })
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

export function getHealth(): Promise<HealthStatus> {
  return apiFetch('/healthz')
}

export function listAdminUsers(): Promise<{ users: AdminUser[] }> {
  return apiFetch('/api/v1/admin/users')
}

export function createAdminUser(req: {
  username: string
  password: string
  is_admin: boolean
}): Promise<AdminUser> {
  return apiFetch('/api/v1/admin/users', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export function patchAdminUser(
  id: number,
  req: { is_admin?: boolean; is_disabled?: boolean },
): Promise<AdminUser> {
  return apiFetch(`/api/v1/admin/users/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
  })
}

export function listAdminInvitations(): Promise<{
  invitations: Invitation[]
}> {
  return apiFetch('/api/v1/admin/invitations')
}

export function createAdminInvitation(): Promise<
  Invitation & { token: string }
> {
  return apiFetch('/api/v1/admin/invitations', { method: 'POST' })
}

export function getAdminBackupStatus(): Promise<AdminBackupStatus> {
  return apiFetch('/api/v1/admin/backups')
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

// --- 認証 (docs/multi-user-design.md Phase A) ---

export interface AuthUser {
  id: number
  username: string
  is_admin: boolean
  instagram_embeds_enabled: boolean
}

// Only present when setup_required is true -- explains why an automatic
// local/remote backup restore didn't happen instead of landing on the
// setup screen, without leaking the actual FR_BACKUP_DIR path or bucket/
// endpoint values (this response is reachable pre-auth).
export interface RestoreHint {
  local_configured: boolean
  local_has_snapshot: boolean
  remote_configured: boolean
  remote_has_snapshot: boolean
  remote_error: boolean
  // Whether POST /api/v1/auth/restore is wired up on this server at all.
  restore_supported: boolean
  // Newest .db snapshot's bare file name (feedla-YYYYMMDD.db) across
  // local/remote and which side it came from -- what restoreFromBackup()
  // would restore. Absent when nothing restorable was found.
  latest_snapshot?: string
  latest_snapshot_source?: 'local' | 'remote'
}

export interface AuthMeResponse {
  authenticated: boolean
  setup_required: boolean
  user?: AuthUser
  restore_hint?: RestoreHint
}

// GET /api/v1/auth/me is the one endpoint reachable without a session, so
// this doubles as "am I logged in" and "does this instance still need
// initial setup" -- see state/auth.ts's checkAuth, called on app boot.
export function getMe(): Promise<AuthMeResponse> {
  return apiFetch('/api/v1/auth/me')
}

// The setup screen's "restore from backup instead of creating a new
// admin" choice. Like setup(), only works while setup is still pending. A
// 202 means the server staged the newest snapshot and is restarting to
// swap it in -- callers should poll getMe() until the restored instance
// answers (see SetupScreen).
export function restoreFromBackup(): Promise<{ status: string }> {
  return apiFetch('/api/v1/auth/restore', { method: 'POST', body: '{}' })
}

// Only succeeds once per instance (server-enforced): after the bootstrap
// admin's password is set, this always 409s. See the setup screen.
export function setup(
  username: string,
  password: string,
): Promise<AuthMeResponse> {
  return apiFetch('/api/v1/auth/setup', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export function login(
  username: string,
  password: string,
): Promise<AuthMeResponse> {
  return apiFetch('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export function logout(): Promise<void> {
  return apiFetch('/api/v1/auth/logout', { method: 'POST' })
}

// Updates the caller's own display settings -- currently just
// instagram_embeds_enabled (see docs/adr/0001-third-party-embed-in-feed-content.md).
// Persisted server-side (not localStorage) so it follows the account
// across devices/browsers.
export function updateMe(settings: {
  instagram_embeds_enabled: boolean
}): Promise<AuthUser> {
  return apiFetch('/api/v1/auth/me', {
    method: 'PATCH',
    body: JSON.stringify(settings),
  })
}

export function changePassword(current: string, next: string): Promise<void> {
  return apiFetch('/api/v1/auth/password', {
    method: 'POST',
    body: JSON.stringify({ current, new: next }),
  })
}

// Reports whether an invitation token is still redeemable, without
// consuming it -- used by the accept screen before showing the signup
// form. The token travels in the body, not the URL, since the server's
// public-path allowlist only matches exact "METHOD path" pairs.
export function getInvitationStatus(token: string): Promise<{
  valid: boolean
}> {
  return apiFetch('/api/v1/invitations/status', {
    method: 'POST',
    body: JSON.stringify({ token }),
  })
}

export function acceptInvitation(
  token: string,
  username: string,
  password: string,
): Promise<AuthMeResponse> {
  return apiFetch('/api/v1/invitations/accept', {
    method: 'POST',
    body: JSON.stringify({ token, username, password }),
  })
}
