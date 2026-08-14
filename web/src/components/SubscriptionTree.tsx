import { useState } from 'preact/hooks'
import type { SubscriptionView } from '../api/types'
import { selectAndLoadFeed, selectGroup } from '../state/actions'
import {
  folders,
  type GroupTarget,
  groupTarget,
  selectedFeedId,
  sidebarViewMode,
  subscriptions,
} from '../state/subscriptions'

const UNFILED_KEY = 0

interface Group {
  id: string
  name: string
  subs: SubscriptionView[]
  target: GroupTarget
}

function isSameGroupTarget(a: GroupTarget | null, b: GroupTarget): boolean {
  if (!a || a.kind !== b.kind) return false
  if (a.kind === 'folder' && b.kind === 'folder') return a.folderId === b.folderId
  if (a.kind === 'rating' && b.kind === 'rating') return a.rating === b.rating
  return false
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
    if (subs) groups.push({ id: `folder-${f.id}`, name: f.name, subs, target: { kind: 'folder', folderId: f.id, label: f.name } })
  }
  const unfiled = byFolder.get(UNFILED_KEY)
  if (unfiled) {
    groups.push({
      id: `folder-${UNFILED_KEY}`,
      name: '(未分類)',
      subs: unfiled,
      target: { kind: 'folder', folderId: null, label: '(未分類)' },
    })
  }
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
    const label = ratingLabel(rating)
    groups.push({ id: `rating-${rating}`, name: label, subs, target: { kind: 'rating', rating, label } })
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
        const isSelected = isSameGroupTarget(groupTarget.value, g.target)
        return (
          <li key={g.id}>
            <div class="folder-row">
              <button
                type="button"
                class="folder-toggle"
                title={isCollapsed ? '展開' : '折りたたむ'}
                onClick={() => toggle(g.id)}
              >
                {isCollapsed ? '▸' : '▾'}
              </button>
              <button
                type="button"
                class={`folder-name${isSelected ? ' selected' : ''}`}
                title="このグループの未読を一気に読む"
                onClick={() => void selectGroup(g.target)}
              >
                {g.name}
              </button>
              <span class="unread-count">{folderUnread > 0 ? folderUnread : ''}</span>
            </div>
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
