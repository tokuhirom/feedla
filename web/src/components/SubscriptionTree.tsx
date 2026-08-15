import { selectAndLoadFeed, selectGroup } from '../state/actions'
import {
  buildGroupsByFolder,
  buildGroupsByPriority,
  collapsedGroups,
  groupTarget,
  groupUnreadCount,
  isSameGroupTarget,
  selectedFeedId,
  sidebarViewMode,
  TODAY_GROUP,
  toggleGroupCollapsed,
} from '../state/subscriptions'
import { faviconUrl } from '../utils/favicon'

export function SubscriptionTree() {
  const groups =
    sidebarViewMode.value === 'priority'
      ? [TODAY_GROUP, ...buildGroupsByPriority()]
      : buildGroupsByFolder()

  return (
    <ul class="subscription-tree">
      {groups.map((g) => {
        const groupUnread = groupUnreadCount(g.target)
        const isSelected = isSameGroupTarget(groupTarget.value, g.target)

        if (g.target.kind === 'today') {
          return (
            <li key={g.id}>
              <div class="folder-row today-row">
                <button
                  type="button"
                  class={`folder-name${isSelected ? ' selected' : ''}`}
                  title="過去24時間の未読をまとめて読む"
                  onClick={() => void selectGroup(g.target)}
                >
                  {g.name}
                </button>
                <span class="unread-count">
                  {groupUnread > 0 ? groupUnread : ''}
                </span>
              </div>
            </li>
          )
        }

        const isCollapsed = collapsedGroups.value[g.id] ?? false
        return (
          <li key={g.id}>
            <div class="folder-row">
              <button
                type="button"
                class="folder-toggle"
                title={isCollapsed ? '展開' : '折りたたむ'}
                onClick={() => toggleGroupCollapsed(g.id)}
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
              <span class="unread-count">
                {groupUnread > 0 ? groupUnread : ''}
              </span>
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
                          ;(
                            e.currentTarget as HTMLImageElement
                          ).style.visibility = 'hidden'
                        }}
                      />
                      <span class="title">{sub.title || sub.feed_url}</span>
                      <span class="unread-count">
                        {sub.unread_count > 0 ? sub.unread_count : ''}
                      </span>
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
