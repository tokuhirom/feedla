import { useEffect, useRef, useState } from 'preact/hooks'
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
  searchOpen,
  showToast,
} from '../state/ui'
import { SubscriptionTree } from './SubscriptionTree'

export function Sidebar() {
  const fileInput = useRef<HTMLInputElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const [menuOpen, setMenuOpen] = useState(false)
  const errorCount = subscriptions.value.filter((s) => s.error_count > 0).length
  // Feedla-side crawl failures (store writes, typically) -- deliberately
  // never counted in errorCount above since they aren't the feed's fault
  // (see crawler.go's InternalErrorEntry doc comment). Surfaced here too,
  // not just inside クロール状況, so one doesn't go unnoticed.
  const internalErrorCount = stats.value?.internal_errors?.length ?? 0

  // Sidebar-wide actions live in this menu instead of many feeds pushing
  // them below the fold (they used to sit in a footer under the
  // subscription list, unreachable without scrolling past every feed).
  useEffect(() => {
    if (!menuOpen) return
    function onPointerDown(e: PointerEvent): void {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    function onKeyDown(e: KeyboardEvent): void {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [menuOpen])

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
          <div class="header-menu" ref={menuRef}>
            <button
              type="button"
              aria-label="メニューを開く"
              aria-haspopup="true"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((v) => !v)}
            >
              ⋮
            </button>
            {menuOpen && (
              <div class="header-menu-dropdown">
                <button
                  type="button"
                  title="記事検索 (/)"
                  onClick={() => {
                    setMenuOpen(false)
                    searchOpen.value = true
                  }}
                >
                  検索
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setMenuOpen(false)
                    statsOpen.value = true
                  }}
                >
                  クロール状況
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setMenuOpen(false)
                    ignoreWordsOpen.value = true
                  }}
                >
                  無視ワード
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setMenuOpen(false)
                    feedManagerOpen.value = true
                  }}
                >
                  フィード管理
                </button>
                <a
                  href="/api/v1/opml"
                  download
                  onClick={() => setMenuOpen(false)}
                >
                  OPML export
                </a>
                <button
                  type="button"
                  onClick={() => {
                    setMenuOpen(false)
                    fileInput.current?.click()
                  }}
                >
                  OPML import
                </button>
              </div>
            )}
            <input
              ref={fileInput}
              type="file"
              accept=".opml,.xml,text/x-opml,text/xml"
              style={{ display: 'none' }}
              onChange={(e) => void onImportFile(e)}
            />
          </div>
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
    </aside>
  )
}
