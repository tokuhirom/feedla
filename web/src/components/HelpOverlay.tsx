import { useEffect } from 'preact/hooks'
import { loadVersion, version } from '../state/health'
import { helpOpen } from '../state/ui'

// One-line summaries for the in-app cheat sheet. The authoritative
// behavior spec (landing rules, what marks entries read, when a key does
// nothing) lives in docs/keyboard-shortcuts.md -- update it alongside any
// change here.
const SHORTCUTS: { key: string; desc: string; implemented: boolean }[] = [
  { key: 'j / k', desc: '次 / 前の記事へ', implemented: true },
  {
    key: 'shift+j',
    desc: '次の記事へ(最後の記事では次に未読がある購読へ)',
    implemented: true,
  },
  {
    key: 'space / shift+space',
    desc: 'ページ単位スクロール',
    implemented: true,
  },
  {
    key: 's / a',
    desc: '次の未読がある購読へ / 直前に読んでいた購読へ戻る',
    implemented: true,
  },
  { key: '+ / -', desc: '購読の評価を上げる / 下げる', implemented: true },
  { key: 'v', desc: '記事を新規タブで開く', implemented: true },
  {
    key: 'r',
    desc: '未読を再取得(サーバへ再クロールを指示)',
    implemented: true,
  },
  {
    key: 'shift+r',
    desc: 'ページ全体を再読込(選択中の購読・読書位置は失われる)',
    implemented: true,
  },
  { key: 'p', desc: 'pin する', implemented: true },
  { key: 'o', desc: 'pin 一覧を開く', implemented: true },
  { key: '/', desc: '検索', implemented: true },
  { key: '?', desc: 'このヘルプを開閉', implemented: true },
]

export function HelpOverlay() {
  useEffect(() => {
    if (helpOpen.value) void loadVersion()
  }, [helpOpen.value])

  if (!helpOpen.value) return null

  return (
    <div class="help-overlay" onClick={() => (helpOpen.value = false)}>
      <div class="help-panel" onClick={(e) => e.stopPropagation()}>
        <h2>キーボードショートカット</h2>
        <ul>
          {SHORTCUTS.map((s) => (
            <li key={s.key} class={s.implemented ? '' : 'not-implemented'}>
              <code>{s.key}</code> {s.desc}
              {!s.implemented && <span class="badge">未実装</span>}
            </li>
          ))}
        </ul>
        <p class="version-info">feedla {version.value ?? '…'}</p>
        <button type="button" onClick={() => (helpOpen.value = false)}>
          閉じる
        </button>
      </div>
    </div>
  )
}
