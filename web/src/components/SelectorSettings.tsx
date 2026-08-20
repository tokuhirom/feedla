import { useState } from 'preact/hooks'
import * as api from '../api/client'
import type { SelectorConfig, SelectorPreviewResult } from '../api/types'
import { authState } from '../state/auth'
import { saveSelectorConfig, scrapeSourceForFeed } from '../state/scrapeSources'
import { subscriptions } from '../state/subscriptions'
import { showToast } from '../state/ui'

interface Props {
  feedId: number
}

function isSelectorPreview(
  res: { blocks: unknown } | SelectorPreviewResult,
): res is SelectorPreviewResult {
  return 'items' in res
}

// The 抽出設定 section of FeedDetailOverlay for a selector subscription
// (design doc §9.3). Mirrors PagewatchSettings's structure: renders nothing
// until scrapeSources has loaded and found this feed's source.
export function SelectorSettings({ feedId }: Props) {
  const source = scrapeSourceForFeed(feedId)
  const me =
    authState.value.status === 'authenticated' ? authState.value.user : null
  // Non-owner subscribers see the form but can't edit or preview it: PATCH
  // and preview are creator-or-admin-only server-side, and 404ing on save
  // is a worse experience than disabling up front (§9.3, §8.2 gap #3).
  const readOnly =
    !me || (source !== undefined && source.created_by !== me.id && !me.is_admin)

  const cfg = (source?.config ?? {}) as SelectorConfig
  const [itemSelector, setItemSelector] = useState(cfg.item_selector ?? '')
  const [linkSelector, setLinkSelector] = useState(cfg.link_selector ?? '')
  const [titleSelector, setTitleSelector] = useState(cfg.title_selector ?? '')
  const [dateSelector, setDateSelector] = useState(cfg.date_selector ?? '')
  const [summarySelector, setSummarySelector] = useState(
    cfg.summary_selector ?? '',
  )
  const [sameHostOnly, setSameHostOnly] = useState(cfg.same_host_only ?? true)
  const [fulltext, setFulltext] = useState(cfg.fulltext ?? true)
  const [maxItemsPerCrawl, setMaxItemsPerCrawl] = useState(
    cfg.max_items_per_crawl ?? 20,
  )
  const [saving, setSaving] = useState(false)
  const [previewing, setPreviewing] = useState(false)
  const [preview, setPreview] = useState<SelectorPreviewResult | null>(null)

  if (!source) return null
  const sourceId = source.id

  function buildConfig(): SelectorConfig {
    return {
      item_selector: itemSelector.trim(),
      link_selector: linkSelector.trim() || undefined,
      title_selector: titleSelector.trim() || undefined,
      date_selector: dateSelector.trim() || undefined,
      summary_selector: summarySelector.trim() || undefined,
      same_host_only: sameHostOnly,
      fulltext,
      max_items_per_crawl: maxItemsPerCrawl,
    }
  }

  async function handleSave(): Promise<void> {
    setSaving(true)
    try {
      const ok = await saveSelectorConfig(feedId, buildConfig())
      if (ok) showToast('抽出設定を保存しました')
    } finally {
      setSaving(false)
    }
  }

  async function handlePreview(): Promise<void> {
    setPreviewing(true)
    setPreview(null)
    try {
      const res = await api.previewScrapeSource(sourceId)
      if (isSelectorPreview(res)) {
        setPreview(res)
      }
    } catch (e) {
      showToast(e instanceof Error ? e.message : String(e))
    } finally {
      setPreviewing(false)
    }
  }

  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  const brokenSelector =
    (sub?.error_count ?? 0) > 0 &&
    (sub?.last_error ?? '').includes('item_selector')

  return (
    <div class="selector-settings">
      <h3>抽出設定</h3>

      {readOnly && (
        <p class="selector-readonly-note">
          この購読の設定は登録した人だけが変更できます。
        </p>
      )}

      {brokenSelector && (
        <p class="selector-broken-warning">
          セレクタにマッチする記事が見つかりませんでした。サイトの構成が変わった可能性があります。プレビューで確認してください。
        </p>
      )}

      <dl class="feed-detail-list">
        <dt>一覧ページ URL</dt>
        <dd>
          <a href={source.target_url} target="_blank" rel="noopener noreferrer">
            {source.target_url}
          </a>
        </dd>
      </dl>

      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!readOnly) void handleSave()
        }}
      >
        <label class="selector-field">
          <span>item_selector（必須）</span>
          <input
            type="text"
            value={itemSelector}
            disabled={readOnly}
            onInput={(e) =>
              setItemSelector((e.target as HTMLInputElement).value)
            }
            placeholder="article"
          />
        </label>
        <label class="selector-field">
          <span>link_selector</span>
          <input
            type="text"
            value={linkSelector}
            disabled={readOnly}
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
            disabled={readOnly}
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
            disabled={readOnly}
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
            disabled={readOnly}
            onInput={(e) =>
              setSummarySelector((e.target as HTMLInputElement).value)
            }
          />
        </label>

        <label class="selector-checkbox">
          <input
            type="checkbox"
            checked={sameHostOnly}
            disabled={readOnly}
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
            disabled={readOnly}
            onChange={(e) =>
              setFulltext((e.target as HTMLInputElement).checked)
            }
          />
          記事ページから本文を取得する
        </label>
        <label class="selector-field">
          <span>1 クロールあたりの記事取得件数上限</span>
          <input
            type="number"
            min={1}
            max={50}
            value={maxItemsPerCrawl}
            disabled={readOnly}
            onInput={(e) =>
              setMaxItemsPerCrawl(
                Number((e.target as HTMLInputElement).value) || 20,
              )
            }
          />
        </label>

        {!readOnly && (
          <div class="dialog-actions">
            <button
              type="submit"
              disabled={saving || itemSelector.trim() === ''}
            >
              {saving ? '保存中…' : '保存'}
            </button>
          </div>
        )}
      </form>

      <div class="selector-preview">
        <button type="button" disabled={previewing} onClick={handlePreview}>
          {previewing ? '取得中…' : 'いま取得して確認'}
        </button>
        {preview && (
          <>
            <p class="pagewatch-section-label">
              マッチ数: {preview.matched}
              {preview.truncated ? '（上限で切り捨てられました）' : ''}
            </p>
            {preview.warnings && preview.warnings.length > 0 && (
              <ul class="selector-preview-warnings">
                {preview.warnings.map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            )}
            <ul class="selector-preview-items">
              {preview.items.map((item) => (
                <li key={item.url} class={item.seen ? 'seen' : undefined}>
                  <a href={item.url} target="_blank" rel="noopener noreferrer">
                    {item.title || item.url}
                  </a>
                  {item.seen && (
                    <span class="selector-preview-seen-badge">取込済</span>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </div>
  )
}
