import { refreshCurrentFeed, selectAndLoadFeed, unsubscribeCurrentFeed } from '../state/actions'
import { adjacentFeedId, clearSelectedFeed, selectedFeedId, subscriptions } from '../state/subscriptions'

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
  const prevFeedId = adjacentFeedId(-1)
  const nextFeedId = adjacentFeedId(1)

  return (
    <header class="entry-header">
      <button type="button" class="back-button" title="購読一覧へ戻る" onClick={() => clearSelectedFeed()}>
        ‹ 一覧
      </button>
      <span class="entry-header-title">{sub.title || sub.feed_url}</span>
      <span class="entry-header-unread">未読 {sub.unread_count}</span>
      <span class="entry-header-rating">{stars}</span>
      <button
        type="button"
        title="前のフィードへ (a)"
        disabled={prevFeedId === null}
        onClick={() => prevFeedId !== null && void selectAndLoadFeed(prevFeedId)}
      >
        ‹
      </button>
      <button
        type="button"
        title="次のフィードへ (s)"
        disabled={nextFeedId === null}
        onClick={() => nextFeedId !== null && void selectAndLoadFeed(nextFeedId)}
      >
        ›
      </button>
      <button type="button" title="再クロール (r)" onClick={() => void refreshCurrentFeed()}>
        ⟳
      </button>
      <button type="button" title="購読解除" onClick={() => void unsubscribeCurrentFeed()}>
        ✕
      </button>
    </header>
  )
}
