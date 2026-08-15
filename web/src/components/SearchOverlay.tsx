import { useState } from 'preact/hooks'
import * as api from '../api/client'
import type { Entry } from '../api/types'
import { setEntryPinned } from '../state/entries'
import { searchOpen } from '../state/ui'

export function SearchOverlay() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Entry[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!searchOpen.value) return null

  function close(): void {
    searchOpen.value = false
    setQuery('')
    setResults(null)
    setError(null)
  }

  async function runSearch(): Promise<void> {
    const q = query.trim()
    if (!q) return
    setLoading(true)
    setError(null)
    try {
      const res = await api.searchEntries(q)
      setResults(res.entries)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  async function togglePin(entry: Entry): Promise<void> {
    try {
      if (entry.pinned) {
        await api.removePin(entry.id)
      } else {
        await api.addPin(entry.id)
      }
      setResults((prev) =>
        prev
          ? prev.map((e) =>
              e.id === entry.id ? { ...e, pinned: !entry.pinned } : e,
            )
          : prev,
      )
      setEntryPinned(entry.id, !entry.pinned)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div class="dialog-overlay" onClick={close}>
      <div
        class="dialog-panel search-panel"
        onClick={(e) => e.stopPropagation()}
      >
        <h2>検索</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void runSearch()
          }}
        >
          <input
            type="text"
            placeholder="キーワード"
            value={query}
            onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
            autoFocus
          />
          <div class="dialog-actions">
            <button type="button" onClick={close}>
              閉じる
            </button>
            <button type="submit" disabled={loading || query.trim() === ''}>
              {loading ? '検索中…' : '検索'}
            </button>
          </div>
        </form>

        {error && <p class="dialog-error">{error}</p>}

        {results && (
          <ul class="search-results">
            {results.length === 0 && (
              <li class="empty-state">見つかりませんでした</li>
            )}
            {results.map((e) => (
              <li key={e.id}>
                <a href={e.url} target="_blank" rel="noopener noreferrer">
                  {e.pinned ? '★ ' : ''}
                  {e.title || '(無題)'}
                </a>
                <button type="button" onClick={() => void togglePin(e)}>
                  {e.pinned ? 'pin解除' : 'pin'}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
