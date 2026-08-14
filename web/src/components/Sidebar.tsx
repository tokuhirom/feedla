import { useRef } from 'preact/hooks'
import * as api from '../api/client'
import { ignoreWordsOpen } from '../state/ignoreWords'
import { statsOpen } from '../state/stats'
import { loadSubscriptions, subscriptions } from '../state/subscriptions'
import { addDialogOpen, errorOverlayOpen, showToast } from '../state/ui'
import { SubscriptionTree } from './SubscriptionTree'

export function Sidebar() {
  const fileInput = useRef<HTMLInputElement>(null)
  const errorCount = subscriptions.value.filter((s) => s.error_count > 0).length

  async function onImportFile(e: Event): Promise<void> {
    const file = (e.target as HTMLInputElement).files?.[0]
    if (!file) return
    try {
      const res = await api.importOpml(file)
      showToast(`${res.imported}件のフィードをインポートしました`)
      await loadSubscriptions()
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err))
    } finally {
      if (fileInput.current) fileInput.current.value = ''
    }
  }

  return (
    <aside class="sidebar">
      <div class="sidebar-header">
        <span>feedla</span>
        <div class="sidebar-header-actions">
          {errorCount > 0 && (
            <button
              type="button"
              class="error-badge"
              title="エラーのあるフィード"
              onClick={() => (errorOverlayOpen.value = true)}
            >
              ⚠ {errorCount}
            </button>
          )}
          <button type="button" title="購読を追加" onClick={() => (addDialogOpen.value = true)}>
            +
          </button>
        </div>
      </div>
      <SubscriptionTree />
      <div class="sidebar-footer">
        <a href="/api/v1/opml" download>
          OPML export
        </a>
        <button type="button" onClick={() => fileInput.current?.click()}>
          OPML import
        </button>
        <button type="button" onClick={() => (statsOpen.value = true)}>
          クロール状況
        </button>
        <button type="button" onClick={() => (ignoreWordsOpen.value = true)}>
          無視ワード
        </button>
        <input
          ref={fileInput}
          type="file"
          accept=".opml,.xml,text/x-opml,text/xml"
          style={{ display: 'none' }}
          onChange={(e) => void onImportFile(e)}
        />
      </div>
    </aside>
  )
}
