import { useState } from 'preact/hooks'
import * as api from '../api/client'
import type { Candidate, SubscriptionView } from '../api/types'
import { loadEntries } from '../state/entries'
import { loadScrapeSources } from '../state/scrapeSources'
import { addSubscription, selectFeed } from '../state/subscriptions'
import { addDialogOpen } from '../state/ui'

export function AddSubscriptionDialog() {
  const [url, setUrl] = useState('')
  const [candidates, setCandidates] = useState<Candidate[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  // Offered when createSubscription 502s (feed.DiscoverFeed found nothing
  // at or linked from the URL) -- design doc §9.1: the fallback is a
  // deliberate, separate user action rather than an automatic retry, so a
  // plain typo'd URL doesn't get silently registered as a page watch.
  const [offerPagewatch, setOfferPagewatch] = useState(false)

  if (!addDialogOpen.value) return null

  function close(): void {
    addDialogOpen.value = false
    setUrl('')
    setCandidates(null)
    setError(null)
    setOfferPagewatch(false)
  }

  function onSubscribed(subscription: SubscriptionView): Promise<void> {
    addSubscription(subscription)
    selectFeed(subscription.feed_id)
    return loadEntries(subscription.feed_id)
  }

  async function submit(targetUrl: string): Promise<void> {
    setSubmitting(true)
    setError(null)
    setOfferPagewatch(false)
    try {
      const res = await api.createSubscription({ url: targetUrl })
      if (res.status === 'candidates') {
        setCandidates(res.candidates)
      } else {
        await onSubscribed(res.subscription)
        close()
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      if (e instanceof api.ApiError && e.status === 502) {
        setOfferPagewatch(true)
      }
    } finally {
      setSubmitting(false)
    }
  }

  async function submitPagewatch(targetUrl: string): Promise<void> {
    setSubmitting(true)
    setError(null)
    try {
      const subscription = await api.createScrapeSource({ url: targetUrl })
      await onSubscribed(subscription)
      await loadScrapeSources()
      close()
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
                  <button
                    type="button"
                    onClick={() => void submit(c.feed_url)}
                    disabled={submitting}
                  >
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

        {/* When offering pagewatch below, its own "このページにフィードが
         * 見つかりませんでした。" line already explains the failure in
         * plain language -- showing the raw {"error":"..."} JSON body
         * above it too would just be noise. */}
        {error && !offerPagewatch && <p class="dialog-error">{error}</p>}

        {offerPagewatch && !candidates && (
          <div class="pagewatch-offer">
            <p>このページにフィードが見つかりませんでした。</p>
            <button
              type="button"
              disabled={submitting}
              onClick={() => void submitPagewatch(url.trim())}
            >
              ページの更新を監視する
            </button>
            <p class="pagewatch-offer-note">
              フィードの代わりに、ページの変化を記事として通知します。
              サイト運営者はフィード配信を意図していません。
              取得間隔は初期値1時間で、以降は更新頻度に応じて自動調整されます
              (最短10分〜最長12時間)。
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
