import type { Entry } from '../api/types'

interface Props {
  entry: Entry
  focused: boolean
}

export function EntryItem({ entry, focused }: Props) {
  const read = entry.read_at != null
  return (
    <article
      id={`entry-${entry.id}`}
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
      {entry.author && <div class="entry-author">{entry.author}</div>}
      {/* body is sanitized server-side (bluemonday) before it ever reaches the client */}
      <div class="entry-body" dangerouslySetInnerHTML={{ __html: entry.body }} />
    </article>
  )
}
