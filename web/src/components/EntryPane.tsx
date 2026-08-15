import { useAutoMarkRead } from '../hooks/useAutoMarkRead'
import { useScrollFocusSync } from '../hooks/useScrollFocusSync'
import { useSwipeNavigation } from '../hooks/useSwipeNavigation'
import { entries, focusedIndex, loadingEntries } from '../state/entries'
import {
  groupTarget,
  ratingLabel,
  selectedFeedId,
  subscriptions,
} from '../state/subscriptions'
import { EntryItem } from './EntryItem'
import { Header } from './Header'

/** Renders entries.value with a rating heading inserted whenever the
 * rating changes -- only meaningful for the Today group, whose
 * entries.value was already physically sorted into rating buckets by
 * state/entries.ts's groupEntriesByRating (see loadGroupEntries), so a
 * simple "rating changed since the previous entry" scan is enough. */
function renderTodayEntries() {
  let lastRating: number | null = null
  return entries.value.map((entry, i) => {
    const rating =
      subscriptions.value.find((s) => s.feed_id === entry.feed_id)?.rating ?? 0
    const heading =
      rating !== lastRating ? (
        <h4 class="today-rating-heading">{ratingLabel(rating)}</h4>
      ) : null
    lastRating = rating
    return (
      <>
        {heading}
        <EntryItem
          key={entry.id}
          entry={entry}
          focused={i === focusedIndex.value}
        />
      </>
    )
  })
}

export function EntryPane() {
  useAutoMarkRead(entries.value.map((e) => e.id))
  useScrollFocusSync()
  useSwipeNavigation()

  if (selectedFeedId.value === null && groupTarget.value === null) {
    return (
      <section class="entry-pane">
        <p class="empty-state">左のサイドバーから購読を選んでください</p>
      </section>
    )
  }

  const isToday = groupTarget.value?.kind === 'today'

  return (
    <section class="entry-pane">
      <Header />
      {loadingEntries.value && <p class="empty-state">読み込み中…</p>}
      {!loadingEntries.value && entries.value.length === 0 && (
        <p class="empty-state">未読はありません</p>
      )}
      {!loadingEntries.value &&
        (isToday
          ? renderTodayEntries()
          : entries.value.map((entry, i) => (
              <EntryItem
                key={entry.id}
                entry={entry}
                focused={i === focusedIndex.value}
              />
            )))}
    </section>
  )
}
