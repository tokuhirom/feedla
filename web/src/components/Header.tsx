import { refreshCurrentFeed, unsubscribeCurrentFeed } from '../state/actions'
import { selectedFeedId, subscriptions } from '../state/subscriptions'

export function Header() {
  const sub = subscriptions.value.find((s) => s.feed_id === selectedFeedId.value)
  if (!sub) {
    return (
      <header class="entry-header">
        <span>購読を選択してください</span>
      </header>
    )
  }

  const stars = '★'.repeat(sub.rating) + '☆'.repeat(Math.max(0, 5 - sub.rating))

  return (
    <header class="entry-header">
      <span class="entry-header-title">{sub.title || sub.feed_url}</span>
      <span class="entry-header-unread">未読 {sub.unread_count}</span>
      <span class="entry-header-rating">{stars}</span>
      <button type="button" title="再クロール (r)" onClick={() => void refreshCurrentFeed()}>
        ⟳
      </button>
      <button type="button" title="購読解除" onClick={() => void unsubscribeCurrentFeed()}>
        ✕
      </button>
    </header>
  )
}
