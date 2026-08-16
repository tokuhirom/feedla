import { useState } from 'preact/hooks'
import * as api from '../api/client'
import type { PreviewBlock } from '../api/types'
import {
  addIgnorePatternRaw,
  removeIgnorePattern,
  scrapeSourceForFeed,
  setWatchMode,
} from '../state/scrapeSources'
import { showToast } from '../state/ui'

interface Props {
  feedId: number
}

// The 監視設定 section of FeedDetailOverlay for a pagewatch subscription
// (design doc §9.3). Renders nothing (not even a "読み込み中") until
// scrapeSources has loaded and found this feed's source -- main.tsx loads
// it eagerly at startup, so in practice this is only ever momentarily
// empty right after registering a brand-new page watch.
export function PagewatchSettings({ feedId }: Props) {
  const [newPattern, setNewPattern] = useState('')
  const [addingPattern, setAddingPattern] = useState(false)
  const [previewBlocks, setPreviewBlocks] = useState<PreviewBlock[] | null>(
    null,
  )
  const [previewing, setPreviewing] = useState(false)

  const source = scrapeSourceForFeed(feedId)
  if (!source) return null

  const sourceId = source.id
  const watchMode = source.config.watch_mode ?? 'additions'
  const ignorePatterns = source.config.ignore_patterns ?? []

  async function handleAddPattern(): Promise<void> {
    const trimmed = newPattern.trim()
    if (!trimmed) return
    setAddingPattern(true)
    try {
      await addIgnorePatternRaw(feedId, trimmed)
      setNewPattern('')
    } finally {
      setAddingPattern(false)
    }
  }

  async function handlePreview(): Promise<void> {
    setPreviewing(true)
    setPreviewBlocks(null)
    try {
      const res = await api.previewScrapeSource(sourceId)
      setPreviewBlocks(res.blocks)
    } catch (e) {
      showToast(e instanceof Error ? e.message : String(e))
    } finally {
      setPreviewing(false)
    }
  }

  return (
    <div class="pagewatch-settings">
      <h3>監視設定</h3>

      <fieldset class="pagewatch-watch-mode">
        <legend>監視モード</legend>
        <label>
          <input
            type="radio"
            name="watch-mode"
            checked={watchMode === 'additions'}
            onChange={() => void setWatchMode(feedId, 'additions')}
          />
          新しく増えた内容だけ通知
        </label>
        <label>
          <input
            type="radio"
            name="watch-mode"
            checked={watchMode === 'changes'}
            onChange={() => void setWatchMode(feedId, 'changes')}
          />
          消えた内容も通知
        </label>
      </fieldset>

      <div class="pagewatch-ignore-patterns">
        <p class="pagewatch-section-label">無視パターン（正規表現）</p>
        {ignorePatterns.length === 0 && (
          <p class="empty-state">無視パターンは登録されていません</p>
        )}
        <ul class="pin-list">
          {ignorePatterns.map((pattern) => (
            <li key={pattern}>
              <code>{pattern}</code>
              <button
                type="button"
                onClick={() => void removeIgnorePattern(feedId, pattern)}
              >
                削除
              </button>
            </li>
          ))}
        </ul>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void handleAddPattern()
          }}
        >
          <input
            type="text"
            placeholder="無視したい正規表現"
            value={newPattern}
            onInput={(e) => setNewPattern((e.target as HTMLInputElement).value)}
          />
          <button
            type="submit"
            disabled={addingPattern || newPattern.trim() === ''}
          >
            追加
          </button>
        </form>
      </div>

      <div class="pagewatch-preview">
        <button type="button" disabled={previewing} onClick={handlePreview}>
          {previewing ? '取得中…' : 'いま取得して確認'}
        </button>
        {previewBlocks && (
          <>
            <p class="pagewatch-section-label">
              ブロック数: {previewBlocks.length}
            </p>
            <ul class="pagewatch-preview-blocks">
              {previewBlocks.map((block, i) => (
                <li key={i} class={block.masked ? 'masked' : undefined}>
                  {block.text || '(空)'}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </div>
  )
}
