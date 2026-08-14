import { useState } from 'preact/hooks'
import * as api from '../api/client'
import type { Candidate } from '../api/types'
import { loadEntries } from '../state/entries'
import { addSubscription, selectFeed } from '../state/subscriptions'
import { addDialogOpen } from '../state/ui'

export function AddSubscriptionDialog() {
  const [url, setUrl] = useState('')
  const [candidates, setCandidates] = useState<Candidate[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  if (!addDialogOpen.value) return null

  function close(): void {
    addDialogOpen.value = false
    setUrl('')
    setCandidates(null)
    setError(null)
  }

  async function submit(targetUrl: string): Promise<void> {
    setSubmitting(true)
    setError(null)
    try {
      const res = await api.createSubscription({ url: targetUrl })
      if (res.status === 'candidates') {
        setCandidates(res.candidates)
      } else {
        addSubscription(res.subscription)
        selectFeed(res.subscription.feed_id)
        await loadEntries(res.subscription.feed_id)
        close()
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div class="dialog-overlay" onClick={close}>
      <div class="dialog-panel" onClick={(e) => e.stopPropagation()}>
        <h2>購読を追加</h2>

        {!candidates && (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              if (url.trim()) void submit(url.trim())
            }}
          >
            <input
              type="text"
              placeholder="https://example.com/feed.xml"
              value={url}
              onInput={(e) => setUrl((e.target as HTMLInputElement).value)}
              autoFocus
            />
            <div class="dialog-actions">
              <button type="button" onClick={close}>
                キャンセル
              </button>
              <button type="submit" disabled={submitting || url.trim() === ''}>
                {submitting ? '追加中…' : '追加'}
              </button>
            </div>
          </form>
        )}

        {candidates && (
          <div>
            <p>複数のフィードが見つかりました。選択してください:</p>
            <ul class="candidate-list">
              {candidates.map((c) => (
                <li key={c.feed_url}>
                  <button type="button" onClick={() => void submit(c.feed_url)} disabled={submitting}>
                    {c.title || c.feed_url}
                  </button>
                </li>
              ))}
            </ul>
            <button type="button" onClick={() => setCandidates(null)}>
              戻る
            </button>
          </div>
        )}

        {error && <p class="dialog-error">{error}</p>}
      </div>
    </div>
  )
}
