import { refreshCurrentFeed, selectAndLoadFeed, setRating } from '../state/actions'
import {
  adjacentFeedId,
  clearSelectedFeed,
  groupTarget,
  groupUnreadCount,
  selectedFeedId,
  subscriptions,
} from '../state/subscriptions'
import { feedDetailOpen } from '../state/ui'

export function Header() {
  if (groupTarget.value) {
    const g = groupTarget.value
    return (
      <header class="entry-header">
        <button type="button" class="back-button" title="購読一覧へ戻る" onClick={() => clearSelectedFeed()}>
          ‹ 一覧
        </button>
        <span class="entry-header-title">{g.label}</span>
        <span class="entry-header-unread">未読 {groupUnreadCount(g)}</span>
      </header>
    )
  }

  const sub = subscriptions.value.find((s) => s.feed_id === selectedFeedId.value)
  if (!sub) {
    return (
      <header class="entry-header">
        <span>購読を選択してください</span>
      </header>
    )
  }

  const prevFeedId = adjacentFeedId(-1)
  const nextFeedId = adjacentFeedId(1)

  return (
    <header class="entry-header">
      <button type="button" class="back-button" title="購読一覧へ戻る" onClick={() => clearSelectedFeed()}>
        ‹ 一覧
      </button>
      <span class="entry-header-title">{sub.title || sub.feed_url}</span>
      <span class="entry-header-unread">未読 {sub.unread_count}</span>
      <span class="entry-header-rating">
        {[1, 2, 3, 4, 5].map((n) => (
          <button
            key={n}
            type="button"
            class="rating-star"
            title={`評価を${n}にする（同じ星をもう一度押すと解除）`}
            onClick={() => void setRating(sub.feed_id, n)}
          >
            {n <= sub.rating ? '★' : '☆'}
          </button>
        ))}
      </span>
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
      <button type="button" title="フィード詳細" onClick={() => (feedDetailOpen.value = true)}>
        詳細
      </button>
    </header>
  )
}
