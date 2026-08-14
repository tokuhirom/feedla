import { unsubscribeFeed } from '../state/actions'
import { subscriptions } from '../state/subscriptions'
import { errorOverlayOpen } from '../state/ui'

export function ErrorFeedsOverlay() {
  if (!errorOverlayOpen.value) return null

  const errored = subscriptions.value.filter((s) => s.error_count > 0)

  return (
    <div class="help-overlay" onClick={() => (errorOverlayOpen.value = false)}>
      <div class="help-panel" onClick={(e) => e.stopPropagation()}>
        <h2>エラーのあるフィード</h2>
        {errored.length === 0 && <p class="empty-state">エラーのあるフィードはありません</p>}
        <ul class="error-feed-list">
          {errored.map((s) => (
            <li key={s.feed_id}>
              <div class="error-feed-title">{s.title || s.feed_url}</div>
              <div class="error-feed-message">
                {s.last_error} ({s.error_count}回連続失敗)
              </div>
              <button type="button" onClick={() => void unsubscribeFeed(s.feed_id)}>
                購読解除
              </button>
            </li>
          ))}
        </ul>
        <button type="button" onClick={() => (errorOverlayOpen.value = false)}>
          閉じる
        </button>
      </div>
    </div>
  )
}
