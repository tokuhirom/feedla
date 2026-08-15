import { useRef } from 'preact/hooks'
import * as api from '../api/client'
import { ignoreWordsOpen } from '../state/ignoreWords'
import { stats, statsOpen } from '../state/stats'
import {
  loadSubscriptions,
  sidebarViewMode,
  subscriptions,
} from '../state/subscriptions'
import {
  addDialogOpen,
  feedManagerInitialOnlyErrors,
  feedManagerOpen,
  showToast,
} from '../state/ui'
import { SubscriptionTree } from './SubscriptionTree'

export function Sidebar() {
  const fileInput = useRef<HTMLInputElement>(null)
  const errorCount = subscriptions.value.filter((s) => s.error_count > 0).length
  // Feedla-side crawl failures (store writes, typically) -- deliberately
  // never counted in errorCount above since they aren't the feed's fault
  // (see crawler.go's InternalErrorEntry doc comment). Surfaced here too,
  // not just inside クロール状況, so one doesn't go unnoticed.
  const internalErrorCount = stats.value?.internal_errors?.length ?? 0

  function openErroringFeeds(): void {
    feedManagerInitialOnlyErrors.value = true
    feedManagerOpen.value = true
  }

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
              onClick={openErroringFeeds}
            >
              ⚠ {errorCount}
            </button>
          )}
          {internalErrorCount > 0 && (
            <button
              type="button"
              class="internal-error-badge"
              title="feedla内部でのクロールエラー(フィード側の問題ではありません)"
              onClick={() => (statsOpen.value = true)}
            >
              🔧 {internalErrorCount}
            </button>
          )}
          <button
            type="button"
            title="購読を追加"
            onClick={() => (addDialogOpen.value = true)}
          >
            +
          </button>
        </div>
      </div>
      <div
        class="view-mode-toggle"
        role="group"
        aria-label="サイドバー表示切り替え"
      >
        <button
          type="button"
          class={sidebarViewMode.value === 'folder' ? 'active' : ''}
          onClick={() => (sidebarViewMode.value = 'folder')}
        >
          カテゴリ
        </button>
        <button
          type="button"
          class={sidebarViewMode.value === 'priority' ? 'active' : ''}
          onClick={() => (sidebarViewMode.value = 'priority')}
        >
          プライオリティ
        </button>
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
        <button type="button" onClick={() => (feedManagerOpen.value = true)}>
          フィード管理
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
