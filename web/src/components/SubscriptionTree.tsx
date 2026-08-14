import { useState } from 'preact/hooks'
import type { SubscriptionView } from '../api/types'
import { selectAndLoadFeed } from '../state/actions'
import { folders, selectedFeedId, sidebarViewMode, subscriptions } from '../state/subscriptions'

const UNFILED_KEY = 0

interface Group {
  id: string
  name: string
  subs: SubscriptionView[]
}

function buildGroupsByFolder(): Group[] {
  const byFolder = new Map<number, SubscriptionView[]>()
  for (const sub of subscriptions.value) {
    const key = sub.folder_id ?? UNFILED_KEY
    const list = byFolder.get(key)
    if (list) {
      list.push(sub)
    } else {
      byFolder.set(key, [sub])
    }
  }

  const sortedFolders = [...folders.value].sort(
    (a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name),
  )

  const groups: Group[] = []
  for (const f of sortedFolders) {
    const subs = byFolder.get(f.id)
    if (subs) groups.push({ id: `folder-${f.id}`, name: f.name, subs })
  }
  const unfiled = byFolder.get(UNFILED_KEY)
  if (unfiled) groups.push({ id: `folder-${UNFILED_KEY}`, name: '(未分類)', subs: unfiled })
  return groups
}

function ratingLabel(rating: number): string {
  return rating === 0 ? '評価なし' : '★'.repeat(rating) + '☆'.repeat(5 - rating)
}

/** Groups by the LDR-style ★ rating (5 down to 0), highest priority first --
 * feedla's "プライオリティモード". */
function buildGroupsByPriority(): Group[] {
  const byRating = new Map<number, SubscriptionView[]>()
  for (const sub of subscriptions.value) {
    const list = byRating.get(sub.rating)
    if (list) {
      list.push(sub)
    } else {
      byRating.set(sub.rating, [sub])
    }
  }

  const groups: Group[] = []
  for (let rating = 5; rating >= 0; rating--) {
    const subs = byRating.get(rating)
    if (!subs) continue
    subs.sort((a, b) => (a.title || a.feed_url).localeCompare(b.title || b.feed_url))
    groups.push({ id: `rating-${rating}`, name: ratingLabel(rating), subs })
  }
  return groups
}

export function SubscriptionTree() {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const groups = sidebarViewMode.value === 'priority' ? buildGroupsByPriority() : buildGroupsByFolder()

  const toggle = (id: string) => setCollapsed((c) => ({ ...c, [id]: !c[id] }))

  return (
    <ul class="subscription-tree">
      {groups.map((g) => {
        const folderUnread = g.subs.reduce((sum, s) => sum + s.unread_count, 0)
        const isCollapsed = collapsed[g.id] ?? false
        return (
          <li key={g.id}>
            <button type="button" class="folder-row" onClick={() => toggle(g.id)}>
              <span>
                {isCollapsed ? '▸' : '▾'} {g.name}
              </span>
              <span class="unread-count">{folderUnread > 0 ? folderUnread : ''}</span>
            </button>
            {!isCollapsed && (
              <ul>
                {g.subs.map((sub) => (
                  <li key={sub.feed_id}>
                    <button
                      type="button"
                      class={`subscription-row${sub.feed_id === selectedFeedId.value ? ' selected' : ''}`}
                      onClick={() => void selectAndLoadFeed(sub.feed_id)}
                    >
                      <span class="title">{sub.title || sub.feed_url}</span>
                      <span class="unread-count">{sub.unread_count > 0 ? sub.unread_count : ''}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </li>
        )
      })}
    </ul>
  )
}
