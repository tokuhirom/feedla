import { addDialogOpen } from '../state/ui'
import { SubscriptionTree } from './SubscriptionTree'

export function Sidebar() {
  return (
    <aside class="sidebar">
      <div class="sidebar-header">
        <span>feedla</span>
        <button type="button" title="購読を追加" onClick={() => (addDialogOpen.value = true)}>
          +
        </button>
      </div>
      <SubscriptionTree />
    </aside>
  )
}
