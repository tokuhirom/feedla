import { useState } from 'preact/hooks'
import * as api from '../api/client'
import type { SubscriptionView } from '../api/types'
import {
  refreshFeed,
  selectAndLoadFeed,
  unsubscribeFeed,
} from '../state/actions'
import {
  clearSelectedFeed,
  folders,
  isErroringFeed,
  removeSubscription,
  subscriptions,
} from '../state/subscriptions'
import {
  feedDetailOpen,
  feedManagerInitialOnlyErrors,
  showErrorToast,
  showToast,
} from '../state/ui'
import { formatUnixSeconds } from '../utils/date'
import { faviconUrl } from '../utils/favicon'

function folderName(folderId: number | null): string {
  if (folderId === null) return '(未分類)'
  return folders.value.find((f) => f.id === folderId)?.name ?? '(未分類)'
}

// Full list of every subscribed feed with a text filter -- lets you check
// whether a feed you half-remember subscribing to (e.g. "ik.am") is actually
// registered, without hunting through folder/priority groups in the sidebar
// by eye. Rendered in the entry pane like a feed/group/search (see
// state/actions.ts's openFeedManager and EntryPane's feedManagerMode
// branch) rather than a modal, so it never remounts mid-session for reasons
// other than actually leaving it -- unlike the old FeedManagerOverlay, that
// means useState(feedManagerInitialOnlyErrors.value) below only needs to
// read the flag once, at construction.
export function FeedManagerPane() {
  const [query, setQuery] = useState('')
  const [onlyErrors, setOnlyErrors] = useState(
    feedManagerInitialOnlyErrors.value,
  )
  const [refreshingIds, setRefreshingIds] = useState<Set<number>>(new Set())
  // Extra narrowing filters + bulk unsubscribe, only surfaced in the ⚠
  // エラーのみ view -- triaging a pile of dead feeds one 購読解除 confirm at
  // a time doesn't scale, but mass-unsubscribing is destructive/irreversible
  // (see unsubscribeFeed's own comment), so this stays scoped to the
  // already-narrower error view rather than the full feed list.
  const [minErrorCount, setMinErrorCount] = useState('')
  const [urlNeedle, setUrlNeedle] = useState('')
  const [errorNeedle, setErrorNeedle] = useState('')
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [bulkDeleting, setBulkDeleting] = useState(false)

  function resetErrorFilters(): void {
    setMinErrorCount('')
    setUrlNeedle('')
    setErrorNeedle('')
    setSelected(new Set())
  }

  function toggleOnlyErrors(): void {
    setOnlyErrors((v) => {
      const next = !v
      if (!next) resetErrorFilters()
      return next
    })
  }

  // selectAndLoadFeed, not a bare selectFeed: opening the detail dialog also
  // hands the entry pane behind it over to that feed, so closing the dialog
  // has to land on the feed's real entry list. Without the load it landed on
  // whatever entries.value happened to hold -- usually nothing, rendering
  // "未読はありません" even for a fully-read feed whose recent entries
  // loadEntries' read fallback would have shown (see state/entries.ts).
  function openDetail(feedId: number): void {
    void selectAndLoadFeed(feedId)
    feedDetailOpen.value = true
  }

  async function handleRefresh(s: SubscriptionView): Promise<void> {
    const label = s.title || s.feed_url
    setRefreshingIds((prev) => new Set(prev).add(s.feed_id))
    try {
      const res = await refreshFeed(s.feed_id)
      if (res.error) {
        showErrorToast(`${label}: ${res.error}`)
      } else {
        showToast(`${label}: 新着 ${res.new_entries} 件`)
      }
    } catch (e) {
      showToast(e instanceof Error ? e.message : String(e))
    } finally {
      setRefreshingIds((prev) => {
        const next = new Set(prev)
        next.delete(s.feed_id)
        return next
      })
    }
  }

  const needle = query.trim().toLowerCase()
  const urlNeedleLower = urlNeedle.trim().toLowerCase()
  const errorNeedleLower = errorNeedle.trim().toLowerCase()
  const minErrorCountNum = Number(minErrorCount)
  const hasMinErrorCount =
    minErrorCount.trim() !== '' && Number.isFinite(minErrorCountNum)
  const errorCount = subscriptions.value.filter(isErroringFeed).length
  const filtered = subscriptions.value
    .filter((s) => (onlyErrors ? isErroringFeed(s) : true))
    .filter((s) => !hasMinErrorCount || s.error_count >= minErrorCountNum)
    .filter(
      (s) =>
        !urlNeedleLower || s.feed_url.toLowerCase().includes(urlNeedleLower),
    )
    .filter(
      (s) =>
        !errorNeedleLower ||
        (s.last_error ?? '').toLowerCase().includes(errorNeedleLower),
    )
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

  const filteredIds = filtered.map((s) => s.feed_id)
  const selectedInView = filteredIds.filter((id) => selected.has(id))
  const allSelected =
    filteredIds.length > 0 && selectedInView.length === filteredIds.length

  function toggleSelectAll(): void {
    setSelected((prev) => {
      const next = new Set(prev)
      for (const id of filteredIds) {
        if (allSelected) next.delete(id)
        else next.add(id)
      }
      return next
    })
  }

  function toggleSelect(feedId: number): void {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(feedId)) next.delete(feedId)
      else next.add(feedId)
      return next
    })
  }

  // One confirm covering every selected feed, rather than unsubscribeFeed's
  // own per-feed confirm -- that's the whole point of bulk selection here.
  // Sequential (not parallel) so a slow/failing delete doesn't pile up
  // concurrent requests against the single write connection (see
  // docs/DESIGN.md's SQLite write-pool note).
  async function handleBulkUnsubscribe(): Promise<void> {
    const targets = filtered.filter((s) => selected.has(s.feed_id))
    if (targets.length === 0) return
    if (
      !window.confirm(
        `選択した ${targets.length} 件の購読を解除しますか？\n記事・pin も削除され、元に戻せません。`,
      )
    ) {
      return
    }

    setBulkDeleting(true)
    let failed = 0
    for (const s of targets) {
      try {
        await api.deleteSubscription(s.feed_id)
        removeSubscription(s.feed_id)
      } catch {
        failed++
      }
    }
    setBulkDeleting(false)
    setSelected(new Set())
    showToast(
      failed > 0
        ? `${targets.length - failed} 件を購読解除しました（${failed} 件失敗）`
        : `${targets.length} 件を購読解除しました`,
    )
  }

  return (
    <section class="entry-pane">
      <header class="entry-header">
        <button
          type="button"
          class="back-button"
          title="購読一覧へ戻る"
          onClick={() => clearSelectedFeed()}
        >
          ‹
        </button>
        <span class="entry-header-title">フィード管理</span>
        <span class="feed-manager-count">
          {filtered.length} / {subscriptions.value.length} 件
        </span>
      </header>
      <div class="feed-manager-body">
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
            onClick={toggleOnlyErrors}
          >
            ⚠ エラーのみ{errorCount > 0 ? ` (${errorCount})` : ''}
          </button>
        </div>
        {onlyErrors && (
          <div class="feed-manager-error-filters">
            <input
              type="text"
              placeholder="URL部分一致"
              value={urlNeedle}
              onInput={(e) =>
                setUrlNeedle((e.target as HTMLInputElement).value)
              }
            />
            <input
              type="text"
              placeholder="エラーメッセージ部分一致"
              value={errorNeedle}
              onInput={(e) =>
                setErrorNeedle((e.target as HTMLInputElement).value)
              }
            />
            <input
              type="number"
              min="0"
              placeholder="エラー回数以上"
              value={minErrorCount}
              onInput={(e) =>
                setMinErrorCount((e.target as HTMLInputElement).value)
              }
            />
          </div>
        )}
        {onlyErrors && filtered.length > 0 && (
          <div class="feed-manager-bulk-bar">
            <label class="feed-manager-select-all">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={toggleSelectAll}
              />
              全選択
            </label>
            <span>{selectedInView.length} 件選択中</span>
            <button
              type="button"
              class="bulk-unsubscribe-button"
              disabled={selectedInView.length === 0 || bulkDeleting}
              onClick={() => void handleBulkUnsubscribe()}
            >
              {bulkDeleting
                ? '解除中…'
                : `選択した ${selectedInView.length} 件を一括購読解除`}
            </button>
          </div>
        )}
        {filtered.length === 0 && (
          <p class="empty-state">該当するフィードはありません</p>
        )}
        <ul class="error-feed-list">
          {filtered.map((s) => (
            <li key={s.feed_id}>
              <div class="feed-manager-row-title">
                {onlyErrors && (
                  <input
                    type="checkbox"
                    class="feed-manager-row-checkbox"
                    checked={selected.has(s.feed_id)}
                    onChange={() => toggleSelect(s.feed_id)}
                  />
                )}
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
                <a href={s.feed_url} target="_blank" rel="noopener noreferrer">
                  {s.feed_url}
                </a>
              </div>
              {s.site_url && (
                <div class="error-feed-site">
                  サイト:{' '}
                  <a
                    href={s.site_url}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {s.site_url}
                  </a>
                </div>
              )}
              {isErroringFeed(s) && (
                <div class="error-feed-message">
                  {s.last_error}（{s.error_count} 回連続失敗）
                </div>
              )}
              <div class="error-feed-time">
                最終取得:{' '}
                {s.last_fetched_at
                  ? formatUnixSeconds(s.last_fetched_at)
                  : '未取得'}
              </div>
              <div class="error-feed-time">
                次回取得予定: {formatUnixSeconds(s.next_fetch_at)}
              </div>
              <div class="error-feed-actions">
                <button type="button" onClick={() => openDetail(s.feed_id)}>
                  詳細
                </button>
                <button
                  type="button"
                  disabled={refreshingIds.has(s.feed_id)}
                  onClick={() => void handleRefresh(s)}
                >
                  {refreshingIds.has(s.feed_id)
                    ? '再クロール中…'
                    : '再クロール'}
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
    </section>
  )
}
