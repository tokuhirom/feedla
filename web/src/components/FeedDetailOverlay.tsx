import { unsubscribeCurrentFeed } from '../state/actions'
import { selectedFeedId, subscriptions } from '../state/subscriptions'
import { feedDetailOpen } from '../state/ui'

function formatUnixSeconds(sec: number): string {
  return new Date(sec * 1000).toLocaleString()
}

// Where 購読解除 (unsubscribe) lives: not a bare icon button in the entry
// header (too easy to mis-tap next to refresh/nav, and "✕" doesn't say what
// it does), but a labeled button on this dedicated detail screen you
// navigate to on purpose.
export function FeedDetailOverlay() {
  if (!feedDetailOpen.value) return null

  const sub = subscriptions.value.find((s) => s.feed_id === selectedFeedId.value)
  if (!sub) return null

  async function handleUnsubscribe(): Promise<void> {
    await unsubscribeCurrentFeed()
    feedDetailOpen.value = false
  }

  return (
    <div class="help-overlay" onClick={() => (feedDetailOpen.value = false)}>
      <div class="help-panel" onClick={(e) => e.stopPropagation()}>
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
                <a href={sub.site_url} target="_blank" rel="noopener noreferrer">
                  {sub.site_url}
                </a>
              </dd>
            </>
          )}
          <dt>最終取得</dt>
          <dd>{sub.last_fetched_at ? formatUnixSeconds(sub.last_fetched_at) : '未取得'}</dd>
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
        <div class="dialog-actions">
          <button type="button" onClick={() => (feedDetailOpen.value = false)}>
            閉じる
          </button>
          <button type="button" class="unsubscribe-button" onClick={() => void handleUnsubscribe()}>
            購読解除
          </button>
        </div>
      </div>
    </div>
  )
}
