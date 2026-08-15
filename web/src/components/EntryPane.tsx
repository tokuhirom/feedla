import { useAutoMarkRead } from '../hooks/useAutoMarkRead'
import { useScrollFocusSync } from '../hooks/useScrollFocusSync'
import { entries, focusedIndex, loadingEntries } from '../state/entries'
import { groupTarget, selectedFeedId } from '../state/subscriptions'
import { EntryItem } from './EntryItem'
import { Header } from './Header'

export function EntryPane() {
  useAutoMarkRead(entries.value.map((e) => e.id))
  useScrollFocusSync()

  if (selectedFeedId.value === null && groupTarget.value === null) {
    return (
      <section class="entry-pane">
        <p class="empty-state">左のサイドバーから購読を選んでください</p>
      </section>
    )
  }

  return (
    <section class="entry-pane">
      <Header />
      {loadingEntries.value && <p class="empty-state">読み込み中…</p>}
      {!loadingEntries.value && entries.value.length === 0 && (
        <p class="empty-state">未読はありません</p>
      )}
      {entries.value.map((entry, i) => (
        <EntryItem key={entry.id} entry={entry} focused={i === focusedIndex.value} />
      ))}
    </section>
  )
}
