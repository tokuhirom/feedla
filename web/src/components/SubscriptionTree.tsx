import { useState } from 'preact/hooks'
import { selectAndLoadFeed, selectGroup } from '../state/actions'
import {
  buildGroupsByFolder,
  buildGroupsByPriority,
  type GroupTarget,
  groupTarget,
  selectedFeedId,
  sidebarViewMode,
} from '../state/subscriptions'
import { faviconUrl } from '../utils/favicon'

function isSameGroupTarget(a: GroupTarget | null, b: GroupTarget): boolean {
  if (!a || a.kind !== b.kind) return false
  if (a.kind === 'folder' && b.kind === 'folder') return a.folderId === b.folderId
  if (a.kind === 'rating' && b.kind === 'rating') return a.rating === b.rating
  return false
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
                      <img
                        class="favicon"
                        src={faviconUrl(sub.site_url || sub.feed_url)}
                        alt=""
                        loading="lazy"
                        onError={(e) => {
                          ;(e.currentTarget as HTMLImageElement).style.visibility = 'hidden'
                        }}
                      />
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
