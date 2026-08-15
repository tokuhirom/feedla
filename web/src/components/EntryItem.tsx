import type { Entry } from '../api/types'
import { selectAndLoadFeed } from '../state/actions'
import { groupTarget, subscriptions } from '../state/subscriptions'
import { formatUnixSeconds } from '../utils/date'

interface Props {
  entry: Entry
  focused: boolean
}

export function EntryItem({ entry, focused }: Props) {
  const read = entry.read_at != null
  const entryDate = entry.updated_at || entry.published_at
  // フォルダ/プライオリティのグループ表示では複数フィードの記事が混ざるので、
  // どのフィードの記事かをここに出し、クリックでそのフィード単体表示 (レート
  // 変更ボタンのあるヘッダー) へ辿れるようにする。
  const feedSub = groupTarget.value
    ? subscriptions.value.find((s) => s.feed_id === entry.feed_id)
    : null
  return (
    <article
      id={`entry-${entry.id}`}
      data-entry-id={entry.id}
      class={`entry-item${focused ? ' focused' : ''}${read ? ' read' : ''}`}
    >
      <h3 class="entry-title">
        {entry.pinned && (
          <span class="pin-star" title="pin済み">
            ★
          </span>
        )}
        <a href={entry.url} target="_blank" rel="noopener noreferrer">
          {entry.title || '(無題)'}
        </a>
      </h3>
      {(feedSub || entry.author || entryDate > 0) && (
        <div class="entry-meta">
          {feedSub && (
            <button
              type="button"
              class="entry-feed-link"
              title="このフィードを開く（評価の変更もここから）"
              onClick={() => void selectAndLoadFeed(feedSub.feed_id)}
            >
              {feedSub.title || feedSub.feed_url}
            </button>
          )}
          {entry.author && <span class="entry-author">{entry.author}</span>}
          {entryDate > 0 && (
            <time
              class="entry-date"
              dateTime={new Date(entryDate * 1000).toISOString()}
            >
              {formatUnixSeconds(entryDate)}
            </time>
          )}
        </div>
      )}
      {/* body is sanitized server-side (bluemonday) before it ever reaches the client */}
      <div
        class="entry-body"
        dangerouslySetInnerHTML={{ __html: entry.body }}
      />
    </article>
  )
}
