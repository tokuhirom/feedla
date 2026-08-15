import { useEffect, useRef, useState } from 'preact/hooks'
import type { Entry } from '../api/types'
import { selectAndLoadFeed } from '../state/actions'
import { groupTarget, subscriptions } from '../state/subscriptions'
import { formatUnixSeconds } from '../utils/date'

// Roughly the height of a Netflix Tech Blog-length post. Below this the
// full body renders inline; above it the body is clamped with a "続きを
// 表示" button so one very long post doesn't dominate the reading pace
// of the entry list. Desktop-only (see the matchMedia check below): j/k
// already advance past a long post one screen at a time, so the button
// would just be a second, redundant way to do the same thing there: mobile
// has no such shortcut key, so it keeps the clamp.
const COLLAPSE_THRESHOLD_PX = 2400

// Mirrors global.css's mobile breakpoint (single-pane layout). Keep in
// sync with that value.
const MOBILE_BREAKPOINT_QUERY = '(max-width: 700px)'

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

  const bodyRef = useRef<HTMLDivElement>(null)
  const [overflowing, setOverflowing] = useState(false)
  const [expanded, setExpanded] = useState(false)

  // ResizeObserver (rather than a one-off scrollHeight read) because
  // .entry-item has content-visibility: auto -- an off-screen entry's
  // true content height isn't known until the browser actually lays it
  // out, which ResizeObserver reliably reports even then.
  useEffect(() => {
    const el = bodyRef.current
    if (!el) return
    if (!window.matchMedia(MOBILE_BREAKPOINT_QUERY).matches) return
    const observer = new ResizeObserver(() => {
      setOverflowing(el.scrollHeight > COLLAPSE_THRESHOLD_PX)
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const collapsed = overflowing && !expanded

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
        ref={bodyRef}
        class={`entry-body${collapsed ? ' entry-body-collapsed' : ''}`}
        style={
          collapsed ? { maxHeight: `${COLLAPSE_THRESHOLD_PX}px` } : undefined
        }
        dangerouslySetInnerHTML={{ __html: entry.body }}
      />
      {collapsed && (
        <button
          type="button"
          class="entry-body-expand"
          onClick={() => setExpanded(true)}
        >
          続きを表示
        </button>
      )}
    </article>
  )
}
