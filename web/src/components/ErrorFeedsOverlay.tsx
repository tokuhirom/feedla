import { unsubscribeFeed } from '../state/actions'
import { selectFeed, subscriptions } from '../state/subscriptions'
import { errorOverlayOpen, feedDetailOpen } from '../state/ui'
import { formatUnixSeconds } from '../utils/date'

export function ErrorFeedsOverlay() {
  if (!errorOverlayOpen.value) return null

  const errored = subscriptions.value.filter((s) => s.error_count > 0)

  // Hands off to FeedDetailOverlay (the same "詳細" screen the entry header
  // links to) instead of duplicating its URL/last-fetched/unsubscribe UI
  // here -- closes this overlay first so the two don't visually stack (see
  // main.tsx's render order).
  function openDetail(feedId: number): void {
    selectFeed(feedId)
    errorOverlayOpen.value = false
    feedDetailOpen.value = true
  }

  return (
    <div
      class="help-overlay error-feed-overlay"
      onClick={() => (errorOverlayOpen.value = false)}
    >
      <div
        class="help-panel error-feed-panel"
        onClick={(e) => e.stopPropagation()}
      >
        <div class="error-feed-panel-header">
          <h2>エラーのあるフィード</h2>
          {/* On phones this list can grow large enough that scrolling to a
           * bottom "閉じる" button is impractical, so the close action lives
           * in a header that stays visible above the scrolling list (see
           * .error-feed-panel-header / .error-feed-list in global.css). */}
          <button
            type="button"
            class="error-feed-close"
            title="閉じる"
            onClick={() => (errorOverlayOpen.value = false)}
          >
            ✕
          </button>
        </div>
        {errored.length === 0 && (
          <p class="empty-state">エラーのあるフィードはありません</p>
        )}
        <ul class="error-feed-list">
          {errored.map((s) => (
            <li key={s.feed_id}>
              <div class="error-feed-title">{s.title || s.feed_url}</div>
              <div class="error-feed-url">{s.feed_url}</div>
              <div class="error-feed-message">
                {s.last_error} ({s.error_count}回連続失敗)
              </div>
              {s.last_fetched_at && (
                <div class="error-feed-time">
                  最終エラー: {formatUnixSeconds(s.last_fetched_at)}
                </div>
              )}
              <div class="error-feed-time">
                次回取得予定: {formatUnixSeconds(s.next_fetch_at)}
              </div>
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
