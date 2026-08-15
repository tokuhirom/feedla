import { useEffect, useState } from 'preact/hooks'
import {
  addIgnoreWord,
  ignoreWords,
  ignoreWordsOpen,
  loadIgnoreWords,
  removeIgnoreWordById,
} from '../state/ignoreWords'
import { showToast } from '../state/ui'

export function IgnoreWordsOverlay() {
  const [word, setWord] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (ignoreWordsOpen.value) void loadIgnoreWords()
  }, [ignoreWordsOpen.value])

  if (!ignoreWordsOpen.value) return null

  async function submit(): Promise<void> {
    const trimmed = word.trim()
    if (!trimmed) return
    setSubmitting(true)
    try {
      await addIgnoreWord(trimmed)
      setWord('')
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div class="dialog-overlay" onClick={() => (ignoreWordsOpen.value = false)}>
      <div class="dialog-panel" onClick={(e) => e.stopPropagation()}>
        <h2>無視ワード</h2>
        <p>
          タイトルまたは本文に含まれる記事を未読一覧・未読数から除外します。
        </p>

        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <input
            type="text"
            placeholder="無視したい単語"
            value={word}
            onInput={(e) => setWord((e.target as HTMLInputElement).value)}
            autoFocus
          />
          <div class="dialog-actions">
            <button type="submit" disabled={submitting || word.trim() === ''}>
              追加
            </button>
          </div>
        </form>

        {ignoreWords.value.length === 0 && (
          <p class="empty-state">無視ワードは登録されていません</p>
        )}
        <ul class="pin-list">
          {ignoreWords.value.map((w) => (
            <li key={w.id}>
              <span>{w.word}</span>
              <button
                type="button"
                onClick={() => void removeIgnoreWordById(w.id)}
              >
                削除
              </button>
            </li>
          ))}
        </ul>

        <div class="dialog-actions">
          <button type="button" onClick={() => (ignoreWordsOpen.value = false)}>
            閉じる
          </button>
        </div>
      </div>
    </div>
  )
}
