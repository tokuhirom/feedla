import { useState } from 'preact/hooks'
import type { SubscriptionView } from '../api/types'
import { selectAndLoadFeed } from '../state/actions'
import { folders, selectedFeedId, subscriptions } from '../state/subscriptions'

const UNFILED_KEY = 0

interface Group {
  id: number
  name: string
  subs: SubscriptionView[]
}

function buildGroups(): Group[] {
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
    if (subs) groups.push({ id: f.id, name: f.name, subs })
  }
  const unfiled = byFolder.get(UNFILED_KEY)
  if (unfiled) groups.push({ id: UNFILED_KEY, name: '(未分類)', subs: unfiled })
  return groups
}

export function SubscriptionTree() {
  const [collapsed, setCollapsed] = useState<Record<number, boolean>>({})
  const groups = buildGroups()

  const toggle = (id: number) => setCollapsed((c) => ({ ...c, [id]: !c[id] }))

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
