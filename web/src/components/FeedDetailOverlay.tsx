import { useState } from 'preact/hooks'
import {
  markFeedReadAll,
  moveFeedToFolder,
  refreshFeed,
  selectAndLoadFeed,
  unsubscribeCurrentFeed,
} from '../state/actions'
import {
  folders,
  sameSiteSubscriptions,
  selectedFeedId,
  subscriptions,
} from '../state/subscriptions'
import { feedDetailOpen, showErrorToast, showToast } from '../state/ui'
import { formatUnixSeconds } from '../utils/date'
import { FulltextSettings } from './FulltextSettings'
import { PagewatchSettings } from './PagewatchSettings'
import { SelectorSettings } from './SelectorSettings'

// Where 購読解除 (unsubscribe) lives: not a bare icon button in the entry
// header (too easy to mis-tap next to refresh/nav, and "✕" doesn't say what
// it does), but a labeled button on this dedicated detail screen you
// navigate to on purpose.
export function FeedDetailOverlay() {
  const [refreshing, setRefreshing] = useState(false)

  if (!feedDetailOpen.value) return null

  const sub = subscriptions.value.find(
    (s) => s.feed_id === selectedFeedId.value,
  )
  if (!sub) return null

  async function handleUnsubscribe(): Promise<void> {
    await unsubscribeCurrentFeed()
    feedDetailOpen.value = false
  }

  const feedId = sub.feed_id
  const label = sub.title || sub.feed_url
  const sameSiteFeeds = sameSiteSubscriptions(sub)

  async function handleReadAll(): Promise<void> {
    await markFeedReadAll(feedId)
  }

  async function handleRefresh(): Promise<void> {
    setRefreshing(true)
    try {
      const res = await refreshFeed(feedId)
      if (res.error) {
        showErrorToast(`${label}: ${res.error}`)
      } else {
        showToast(`${label}: 新着 ${res.new_entries} 件`)
      }
    } catch (e) {
      showToast(e instanceof Error ? e.message : String(e))
    } finally {
      setRefreshing(false)
    }
  }

  async function handleFolderChange(e: Event): Promise<void> {
    const value = (e.target as HTMLSelectElement).value
    await moveFeedToFolder(feedId, value === '' ? null : Number(value))
  }

  return (
    <div class="help-overlay" onClick={() => (feedDetailOpen.value = false)}>
      <div
        class={`help-panel${sub.kind === 'pagewatch' || sub.kind === 'selector' ? ' help-panel-wide' : ''}`}
        onClick={(e) => e.stopPropagation()}
      >
        <h2>{sub.title || sub.feed_url}</h2>
        <dl class="feed-detail-list">
          <dt>フィード URL</dt>
          <dd>
            <a href={sub.feed_url} target="_blank" rel="noopener noreferrer">
              {sub.feed_url}
            </a>
          </dd>
          {sub.site_url && (
            <>
              <dt>サイト URL</dt>
              <dd>
                <a
                  href={sub.site_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {sub.site_url}
                </a>
              </dd>
            </>
          )}
          <dt>カテゴリ</dt>
          <dd>
            <select
              value={sub.folder_id ?? ''}
              onChange={(e) => void handleFolderChange(e)}
            >
              <option value="">(未分類)</option>
              {folders.value.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
                </option>
              ))}
            </select>
          </dd>
          <dt>最終取得</dt>
          <dd>
            {sub.last_fetched_at
              ? formatUnixSeconds(sub.last_fetched_at)
              : '未取得'}
          </dd>
          <dt>次回取得予定</dt>
          <dd>{formatUnixSeconds(sub.next_fetch_at)}</dd>
          {sub.error_count > 0 && (
            <>
              <dt>直近のエラー</dt>
              <dd>
                {sub.last_error}（{sub.error_count} 回連続失敗）
              </dd>
            </>
          )}
        </dl>
        {sameSiteFeeds.length > 0 && (
          <div class="same-site-feeds">
            <p>
              同じサイト URL の別フィードが {sameSiteFeeds.length}{' '}
              件あります（重複購読の可能性）：
            </p>
            <ul>
              {sameSiteFeeds.map((s) => (
                <li key={s.feed_id}>
                  <button
                    type="button"
                    onClick={() => void selectAndLoadFeed(s.feed_id)}
                  >
                    {s.title || s.feed_url}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
        {sub.kind === 'pagewatch' && <PagewatchSettings feedId={feedId} />}
        {sub.kind === 'selector' && <SelectorSettings feedId={feedId} />}
        {sub.kind === 'feed' && <FulltextSettings feedId={feedId} />}
        <div class="dialog-actions">
          <button type="button" onClick={() => (feedDetailOpen.value = false)}>
            閉じる
          </button>
          <button
            type="button"
            disabled={sub.unread_count === 0}
            onClick={() => void handleReadAll()}
          >
            全て既読にする
          </button>
          <button
            type="button"
            disabled={refreshing}
            onClick={() => void handleRefresh()}
          >
            {refreshing ? '再クロール中…' : '再クロール'}
          </button>
          <button
            type="button"
            class="unsubscribe-button"
            onClick={() => void handleUnsubscribe()}
          >
            購読解除
          </button>
        </div>
      </div>
    </div>
  )
}
