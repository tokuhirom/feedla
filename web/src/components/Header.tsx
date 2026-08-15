import {
  refreshCurrentFeed,
  selectAndLoadFeed,
  setRating,
} from '../state/actions'
import { entries, focusedIndex } from '../state/entries'
import {
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
    // A folder/priority group mixes entries from many feeds -- once a long
    // entry's body scrolls the per-entry feed link (see EntryItem) out of
    // view, there's nothing left on screen saying whose article this is.
    // Mirror the focused entry's feed here in the sticky header instead, so
    // it stays visible for as long as that entry does.
    const focusedEntry = entries.value[focusedIndex.value]
    const focusedSub = focusedEntry
      ? subscriptions.value.find((s) => s.feed_id === focusedEntry.feed_id)
      : null
    return (
      <header class="entry-header">
        <button
          type="button"
          class="back-button"
          title="購読一覧へ戻る"
          onClick={() => clearSelectedFeed()}
        >
          ‹
        </button>
        <span class="entry-header-title">
          {g.label}
          {focusedSub && (
            <>
              {' › '}
              <button
                type="button"
                class="entry-header-current-feed"
                title="このフィードを開く（評価の変更もここから）"
                onClick={() => void selectAndLoadFeed(focusedSub.feed_id)}
              >
                {focusedSub.title || focusedSub.feed_url}
              </button>
            </>
          )}
        </span>
        <span class="entry-header-unread">
          (<span class="entry-header-unread-count">{groupUnreadCount(g)}</span>)
        </span>
      </header>
    )
  }

  const sub = subscriptions.value.find(
    (s) => s.feed_id === selectedFeedId.value,
  )
  if (!sub) {
    return (
      <header class="entry-header">
        <span>購読を選択してください</span>
      </header>
    )
  }

  return (
    <header class="entry-header">
      <button
        type="button"
        class="back-button"
        title="購読一覧へ戻る"
        onClick={() => clearSelectedFeed()}
      >
        ‹
      </button>
      <span class="entry-header-title">{sub.title || sub.feed_url}</span>
      <span class="entry-header-unread">
        (<span class="entry-header-unread-count">{sub.unread_count}</span>)
      </span>
      <span class="entry-header-tools">
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
          title="再クロール (r)"
          onClick={() => void refreshCurrentFeed()}
        >
          ⟳
        </button>
        <button
          type="button"
          title="フィード詳細"
          onClick={() => (feedDetailOpen.value = true)}
        >
          詳細
        </button>
      </span>
    </header>
  )
}
