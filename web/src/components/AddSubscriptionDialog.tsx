import { useState } from 'preact/hooks'
import * as api from '../api/client'
import type {
  Candidate,
  SelectorConfig,
  SelectorPreviewResult,
  SubscriptionView,
} from '../api/types'
import { loadEntries } from '../state/entries'
import { loadScrapeSources } from '../state/scrapeSources'
import { addSubscription, selectFeed } from '../state/subscriptions'
import { addDialogOpen } from '../state/ui'

function isSelectorPreview(
  res: { blocks: unknown } | SelectorPreviewResult,
): res is SelectorPreviewResult {
  return 'items' in res
}

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

  // §9.1's second fallback: "記事一覧として取り込む" (selector, 方式B1).
  // selectorMode gates the multi-step CSS-selector-then-preview form below;
  // its own fields/preview state are separate from the plain-URL form above
  // them since they only exist once the user has committed to this path.
  const [selectorMode, setSelectorMode] = useState(false)
  const [itemSelector, setItemSelector] = useState('')
  const [linkSelector, setLinkSelector] = useState('')
  const [titleSelector, setTitleSelector] = useState('')
  const [dateSelector, setDateSelector] = useState('')
  const [summarySelector, setSummarySelector] = useState('')
  const [sameHostOnly, setSameHostOnly] = useState(true)
  const [fulltext, setFulltext] = useState(true)
  const [previewing, setPreviewing] = useState(false)
  const [selectorPreview, setSelectorPreview] =
    useState<SelectorPreviewResult | null>(null)

  if (!addDialogOpen.value) return null

  function close(): void {
    addDialogOpen.value = false
    setUrl('')
    setCandidates(null)
    setError(null)
    setOfferPagewatch(false)
    setSelectorMode(false)
    setItemSelector('')
    setLinkSelector('')
    setTitleSelector('')
    setDateSelector('')
    setSummarySelector('')
    setSameHostOnly(true)
    setFulltext(true)
    setSelectorPreview(null)
  }

  function onSubscribed(subscription: SubscriptionView): Promise<void> {
    addSubscription(subscription)
    selectFeed(subscription.feed_id)
    return loadEntries(subscription.feed_id)
  }

  // opts is only passed when confirming a specific candidate (see
  // handleCreateSubscription): omitted, this is the initial "discover what's
  // at this URL" call, which always comes back with a candidate list (even
  // a single-feed site) rather than subscribing directly -- see
  // candidates.map below. title carries the candidate's discovered feed
  // title through to feed creation -- without it, a feed whose very first
  // crawl fails (e.g. the target went down right after subscribing) would
  // never get a real title at all, since crawlOne only overwrites it on a
  // successful crawl.
  async function submit(
    targetUrl: string,
    opts?: { confirmed?: boolean; fulltext?: boolean; title?: string },
  ): Promise<void> {
    setSubmitting(true)
    setError(null)
    setOfferPagewatch(false)
    try {
      const res = await api.createSubscription({
        url: targetUrl,
        confirmed: opts?.confirmed,
        fulltext: opts?.fulltext,
        title: opts?.title,
      })
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

  function buildSelectorConfig(): SelectorConfig {
    return {
      item_selector: itemSelector.trim(),
      link_selector: linkSelector.trim() || undefined,
      title_selector: titleSelector.trim() || undefined,
      date_selector: dateSelector.trim() || undefined,
      summary_selector: summarySelector.trim() || undefined,
      same_host_only: sameHostOnly,
      fulltext,
    }
  }

  async function handleSelectorPreview(): Promise<void> {
    setPreviewing(true)
    setSelectorPreview(null)
    setError(null)
    try {
      const res = await api.previewUnsavedScrapeSource({
        kind: 'selector',
        url: url.trim(),
        config: buildSelectorConfig(),
      })
      if (isSelectorPreview(res)) {
        setSelectorPreview(res)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setPreviewing(false)
    }
  }

  async function submitSelector(): Promise<void> {
    setSubmitting(true)
    setError(null)
    try {
      const subscription = await api.createScrapeSource({
        kind: 'selector',
        url: url.trim(),
        config: buildSelectorConfig(),
      })
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

        {!candidates && !selectorMode && (
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
            <p>購読方法を選択してください:</p>
            <ul class="candidate-list">
              {candidates.map((c) => (
                <li key={`${c.feed_url}::${c.fulltext}`}>
                  <button
                    type="button"
                    onClick={() =>
                      void submit(c.feed_url, {
                        confirmed: true,
                        fulltext: c.fulltext,
                        title: c.title,
                      })
                    }
                    disabled={submitting}
                  >
                    {c.title || c.feed_url}
                    {c.fulltext ? ' (本文抽出あり)' : ''}
                  </button>
                </li>
              ))}
            </ul>
            <button type="button" onClick={() => setCandidates(null)}>
              戻る
            </button>
          </div>
        )}

        {/* When offering pagewatch/selector below, their own explanatory
         * lines already say what happened in plain language -- showing the
         * raw {"error":"..."} JSON body above them too would just be noise. */}
        {error && !offerPagewatch && !selectorMode && (
          <p class="dialog-error">{error}</p>
        )}

        {offerPagewatch && !candidates && !selectorMode && (
          <div class="pagewatch-offer">
            <p>このページにフィードが見つかりませんでした。</p>
            <div class="scrape-offer-choices">
              <button
                type="button"
                disabled={submitting}
                onClick={() => setSelectorMode(true)}
              >
                記事一覧として取り込む
              </button>
              <button
                type="button"
                disabled={submitting}
                onClick={() => void submitPagewatch(url.trim())}
              >
                ページの更新を監視する
              </button>
            </div>
            <p class="pagewatch-offer-note">
              「記事一覧として取り込む」は、お知らせ一覧やブログのトップなど、
              記事ごとに個別ページを持つ一覧を CSS セレクタで指定して記事単位で
              読めるようにします。「ページの更新を監視する」は、ページ全体の
              変化を1件の記事として通知します。いずれもサイト運営者はフィード
              配信を意図していません。取得間隔は初期値1時間で、以降は更新頻度に
              応じて自動調整されます(最短10分〜最長12時間)。
            </p>
          </div>
        )}

        {selectorMode && (
          <div class="selector-offer">
            <p>
              一覧ページから記事を抜き出す CSS セレクタを指定してください。
              まずプレビューで確認してから購読できます。
            </p>
            {error && <p class="dialog-error">{error}</p>}
            <label class="selector-field">
              <span>item_selector（必須。記事1件に相当する繰り返し要素）</span>
              <input
                type="text"
                value={itemSelector}
                placeholder="article"
                onInput={(e) =>
                  setItemSelector((e.target as HTMLInputElement).value)
                }
                autoFocus
              />
            </label>
            <details class="selector-advanced">
              <summary>詳細設定</summary>
              <label class="selector-field">
                <span>link_selector</span>
                <input
                  type="text"
                  value={linkSelector}
                  onInput={(e) =>
                    setLinkSelector((e.target as HTMLInputElement).value)
                  }
                />
              </label>
              <label class="selector-field">
                <span>title_selector</span>
                <input
                  type="text"
                  value={titleSelector}
                  onInput={(e) =>
                    setTitleSelector((e.target as HTMLInputElement).value)
                  }
                />
              </label>
              <label class="selector-field">
                <span>date_selector</span>
                <input
                  type="text"
                  value={dateSelector}
                  onInput={(e) =>
                    setDateSelector((e.target as HTMLInputElement).value)
                  }
                />
              </label>
              <label class="selector-field">
                <span>summary_selector</span>
                <input
                  type="text"
                  value={summarySelector}
                  onInput={(e) =>
                    setSummarySelector((e.target as HTMLInputElement).value)
                  }
                />
              </label>
              <label class="selector-checkbox">
                <input
                  type="checkbox"
                  checked={sameHostOnly}
                  onChange={(e) =>
                    setSameHostOnly((e.target as HTMLInputElement).checked)
                  }
                />
                一覧ページと同じホストのリンクだけ取り込む
              </label>
              <label class="selector-checkbox">
                <input
                  type="checkbox"
                  checked={fulltext}
                  onChange={(e) =>
                    setFulltext((e.target as HTMLInputElement).checked)
                  }
                />
                記事ページから本文を取得する
              </label>
            </details>

            <div class="dialog-actions">
              <button
                type="button"
                onClick={() => {
                  setSelectorMode(false)
                  setSelectorPreview(null)
                }}
              >
                戻る
              </button>
              <button
                type="button"
                disabled={previewing || itemSelector.trim() === ''}
                onClick={() => void handleSelectorPreview()}
              >
                {previewing ? 'プレビュー中…' : 'プレビュー'}
              </button>
            </div>

            {selectorPreview && (
              <div class="selector-preview">
                <p class="pagewatch-section-label">
                  マッチ数: {selectorPreview.matched}
                  {selectorPreview.truncated
                    ? '（上限で切り捨てられました）'
                    : ''}
                </p>
                {selectorPreview.warnings &&
                  selectorPreview.warnings.length > 0 && (
                    <ul class="selector-preview-warnings">
                      {selectorPreview.warnings.map((w) => (
                        <li key={w}>{w}</li>
                      ))}
                    </ul>
                  )}
                {selectorPreview.items.length === 0 && (
                  <p class="empty-state">マッチする記事がありません</p>
                )}
                <ul class="selector-preview-items">
                  {selectorPreview.items.slice(0, 20).map((item) => (
                    <li key={item.url}>
                      <a
                        href={item.url}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        {item.title || item.url}
                      </a>
                    </li>
                  ))}
                </ul>
                {selectorPreview.items.length > 0 && (
                  <div class="dialog-actions">
                    <button
                      type="button"
                      disabled={submitting}
                      onClick={() => void submitSelector()}
                    >
                      {submitting ? '購読中…' : 'この設定で購読'}
                    </button>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
