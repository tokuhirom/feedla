import { useEffect, useState } from 'preact/hooks'
import { unsubscribeFeed } from '../state/actions'
import { folders, selectFeed, subscriptions } from '../state/subscriptions'
import {
  feedDetailOpen,
  feedManagerInitialOnlyErrors,
  feedManagerOpen,
} from '../state/ui'
import { faviconUrl } from '../utils/favicon'

function folderName(folderId: number | null): string {
  if (folderId === null) return '(未分類)'
  return folders.value.find((f) => f.id === folderId)?.name ?? '(未分類)'
}

// Full list of every subscribed feed with a text filter -- lets you check
// whether a feed you half-remember subscribing to (e.g. "ik.am") is actually
// registered, without hunting through folder/priority groups in the sidebar
// by eye. Also reachable pre-filtered from the sidebar's ⚠ badge (see
// feedManagerInitialOnlyErrors in state/ui.ts).
export function FeedManagerOverlay() {
  const [query, setQuery] = useState('')
  const [onlyErrors, setOnlyErrors] = useState(false)

  // Like the other overlays in main.tsx, this component stays mounted the
  // whole app lifetime and toggles visibility via the early return below --
  // it never actually unmounts, so useState(feedManagerInitialOnlyErrors)
  // would only capture that signal's value once, at the very first (closed)
  // render. Re-sync on every open transition instead, so the sidebar's ⚠
  // badge (see Sidebar.tsx) can reliably open this overlay pre-filtered.
  useEffect(() => {
    if (feedManagerOpen.value) setOnlyErrors(feedManagerInitialOnlyErrors.value)
  }, [feedManagerOpen.value])

  if (!feedManagerOpen.value) return null

  function close(): void {
    feedManagerOpen.value = false
    feedManagerInitialOnlyErrors.value = false
    setQuery('')
    setOnlyErrors(false)
  }

  function openDetail(feedId: number): void {
    selectFeed(feedId)
    feedManagerOpen.value = false
    feedManagerInitialOnlyErrors.value = false
    feedDetailOpen.value = true
  }

  const needle = query.trim().toLowerCase()
  const errorCount = subscriptions.value.filter((s) => s.error_count > 0).length
  const filtered = subscriptions.value
    .filter((s) => (onlyErrors ? s.error_count > 0 : true))
    .filter((s) => {
      if (!needle) return true
      return (
        s.title.toLowerCase().includes(needle) ||
        s.feed_url.toLowerCase().includes(needle) ||
        (s.site_url ?? '').toLowerCase().includes(needle)
      )
    })
    .sort((a, b) =>
      (a.title || a.feed_url).localeCompare(b.title || b.feed_url),
    )

  return (
    <div class="help-overlay error-feed-overlay" onClick={close}>
      <div
        class="help-panel error-feed-panel"
        onClick={(e) => e.stopPropagation()}
      >
        <div class="error-feed-panel-header">
          <h2>フィード管理</h2>
          <button
            type="button"
            class="error-feed-close"
            title="閉じる"
            onClick={close}
          >
            ✕
          </button>
        </div>
        <input
          type="text"
          class="feed-manager-search"
          placeholder="タイトル・URLで絞り込み"
          value={query}
          onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
          autoFocus
        />
        <div class="feed-manager-toolbar">
          <button
            type="button"
            class={onlyErrors ? 'active' : ''}
            disabled={errorCount === 0}
            onClick={() => setOnlyErrors((v) => !v)}
          >
            ⚠ エラーのみ{errorCount > 0 ? ` (${errorCount})` : ''}
          </button>
          <span class="feed-manager-count">
            {filtered.length} / {subscriptions.value.length} 件
          </span>
        </div>
        {filtered.length === 0 && (
          <p class="empty-state">該当するフィードはありません</p>
        )}
        <ul class="error-feed-list">
          {filtered.map((s) => (
            <li key={s.feed_id}>
              <div class="feed-manager-row-title">
                <img
                  class="favicon"
                  src={faviconUrl(s.site_url || s.feed_url)}
                  alt=""
                  loading="lazy"
                  onError={(e) => {
                    ;(e.currentTarget as HTMLImageElement).style.visibility =
                      'hidden'
                  }}
                />
                <span class="error-feed-title">{s.title || s.feed_url}</span>
                <span class="unread-count">
                  {s.unread_count > 0 ? s.unread_count : ''}
                </span>
              </div>
              <div class="error-feed-folder">
                {folderName(s.folder_id ?? null)}
              </div>
              <div class="error-feed-url">
                <a
                  href={s.site_url || s.feed_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {s.feed_url}
                </a>
              </div>
              {s.error_count > 0 && (
                <div class="error-feed-message">
                  {s.last_error}（{s.error_count} 回連続失敗）
                </div>
              )}
              <div class="error-feed-actions">
                <button type="button" onClick={() => openDetail(s.feed_id)}>
                  詳細
                </button>
                <button
                  type="button"
                  onClick={() => void unsubscribeFeed(s.feed_id)}
                >
                  購読解除
                </button>
              </div>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
