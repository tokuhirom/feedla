import { useEffect, useRef, useState } from 'preact/hooks'
import * as api from '../api/client'
import type { InspectElement, InspectResult } from '../api/types'
import {
  findContainingItem,
  generateItemSelectorCandidates,
  generateTitleSelectorCandidates,
  type SelectorCandidate,
} from '../lib/selectorGen'

interface Props {
  url: string
  onApply: (result: { itemSelector: string; titleSelector?: string }) => void
  onClose: () => void
}

// Swatch colors for candidate rows, index-synced with HIGHLIGHT_COLORS in
// internal/inspect/picker.go: the picker script frames candidate i's
// matched elements in the same color as row i's swatch. Deliberately
// distinct hues from the script's blue hover outline.
const CANDIDATE_COLORS = ['#ea580c', '#0891b2', '#9333ea']

// Phase F2's click-to-selector GUI (design doc §10). Fetches a sanitized,
// single-use view of url (POST /scrape_sources/inspect) and embeds it in a
// sandboxed iframe with no allow-same-origin -- this component can never
// read the iframe's content or DOM, only the integer element id the
// page's fixed picker script posts back via postMessage. Every selector
// string shown here is reconstructed client-side purely from the
// server-supplied structural index (lib/selectorGen.ts), never from
// anything read out of the frame. The caller (AddSubscriptionDialog /
// SelectorSettings) still has to run the result through the existing
// server-side preview before subscribing -- this is a convenience
// generator, not an authority (§10.6, §10.7).
export function SelectorPicker({ url, onApply, onClose }: Props) {
  // session wraps the currently live inspect token/element-index pair. A
  // fresh session is requested every time this component mounts and every
  // time the user hits "再読み込み" -- never reused -- because the view
  // token is single-use server-side: replaying an old view_url (e.g. from
  // a stale closure) would just 404 with no way for this component to
  // notice (the iframe is cross-origin/opaque, so its response body isn't
  // readable from here).
  const [session, setSession] = useState<InspectResult | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [itemCandidates, setItemCandidates] = useState<
    SelectorCandidate[] | null
  >(null)
  const [chosenItem, setChosenItem] = useState<SelectorCandidate | null>(null)
  const [titleCandidates, setTitleCandidates] = useState<
    SelectorCandidate[] | null
  >(null)
  const [hint, setHint] = useState<string | null>(null)

  const iframeRef = useRef<HTMLIFrameElement | null>(null)
  // Mirrors `session` for the message-event handler below: the handler is
  // registered once per session (see the effect's dep array) but reads
  // sessionRef.current instead of closing over `session` so a click that
  // arrives in the middle of a state update never validates against a
  // stale element index.
  const sessionRef = useRef<InspectResult | null>(null)
  sessionRef.current = session
  // Same staleness concern as sessionRef, for a different reason: the
  // message handler below is (re)installed only when `session` changes,
  // so if it read `chosenItem` directly it would keep seeing whatever
  // value was current at install time -- e.g. still null after the user
  // has since picked an item, misrouting their next click back into
  // item-candidate generation instead of title-candidate generation.
  const chosenItemRef = useRef<SelectorCandidate | null>(null)
  chosenItemRef.current = chosenItem

  async function load(): Promise<void> {
    setLoading(true)
    setLoadError(null)
    setSession(null)
    setItemCandidates(null)
    setChosenItem(null)
    setTitleCandidates(null)
    setHint(null)
    try {
      const result = await api.inspectScrapeSource(url)
      setSession(result)
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    // Deliberately mount-only: this component is unmounted/remounted by
    // its caller on reopen, and load() is also reachable via the
    // "再読み込み" button below -- see the module doc comment on session.
  }, [])

  useEffect(() => {
    function handleMessage(ev: MessageEvent): void {
      const iframe = iframeRef.current
      // event.source identity, not event.origin -- a sandboxed iframe
      // without allow-same-origin has an opaque origin, so event.origin
      // is always the literal string "null" here and can't distinguish
      // this frame from any other opaque-origin frame on the page
      // (design doc §10.5).
      if (!iframe || ev.source !== iframe.contentWindow) return
      const data = ev.data as unknown
      if (
        !data ||
        typeof data !== 'object' ||
        (data as { type?: unknown }).type !== 'feedla-inspect-click'
      ) {
        return
      }
      const id = (data as { id?: unknown }).id
      if (typeof id !== 'number') return
      const cur = sessionRef.current
      if (!cur) return
      // Cross-check against the server-supplied index before trusting the
      // id for anything -- the picker script can only ever have gotten a
      // data-feedla-id the server itself assigned, but this keeps the
      // handler defensive regardless.
      if (!cur.elements.some((el) => el.id === id)) return
      handleElementClick(id, cur.elements)
    }
    window.addEventListener('message', handleMessage)
    return () => window.removeEventListener('message', handleMessage)
    // Re-bind whenever the session (and therefore its element index)
    // changes, so a leftover handler from a previous session can never
    // validate a click against a stale index -- see sessionRef above.
  }, [session])

  // Mirror whatever candidate list is currently on screen into the iframe
  // as colored frames, so "マッチ数: 386" is visibly "every nav link" and
  // not just a number. One-way and non-secret: only the server-assigned
  // integer ids go in, and the picker script's reaction is purely visual
  // inside its own document. targetOrigin must be '*' -- the sandboxed
  // frame has an opaque origin (§10.5), so no concrete origin can name it.
  useEffect(() => {
    const frameWindow = iframeRef.current?.contentWindow
    if (!session || !frameWindow) return
    let groups: number[][] = []
    if (itemCandidates) {
      groups = itemCandidates.map((c) => c.matchedIds)
    } else if (titleCandidates) {
      groups = titleCandidates.map((c) => c.matchedIds)
    } else if (chosenItem) {
      groups = [chosenItem.matchedIds]
    }
    frameWindow.postMessage({ type: 'feedla-inspect-highlight', groups }, '*')
  }, [session, itemCandidates, titleCandidates, chosenItem])

  function handleElementClick(id: number, elements: InspectElement[]): void {
    setHint(null)
    const currentItem = chosenItemRef.current
    if (!currentItem) {
      setItemCandidates(generateItemSelectorCandidates(elements, id))
      return
    }
    const itemId = findContainingItem(elements, currentItem.matchedIds, id)
    if (itemId == null) {
      setHint(
        '選択した記事の範囲外です。マッチした記事の中の要素をクリックしてください。',
      )
      return
    }
    setTitleCandidates(generateTitleSelectorCandidates(elements, itemId, id))
  }

  function chooseItem(candidate: SelectorCandidate): void {
    setChosenItem(candidate)
    setItemCandidates(null)
    setTitleCandidates(null)
    setHint(null)
  }

  function restartItemChoice(): void {
    setChosenItem(null)
    setItemCandidates(null)
    setTitleCandidates(null)
    setHint(null)
  }

  function finish(titleSelector?: string): void {
    if (!chosenItem) return
    onApply({ itemSelector: chosenItem.selector, titleSelector })
    onClose()
  }

  return (
    <div class="dialog-overlay" onClick={onClose}>
      <div
        class="dialog-panel selector-picker-panel"
        onClick={(e) => e.stopPropagation()}
      >
        <h2>ページから選ぶ</h2>

        {loading && <p>読み込み中…</p>}

        {loadError && (
          <div>
            <p class="dialog-error">{loadError}</p>
            <div class="dialog-actions">
              <button type="button" onClick={onClose}>
                キャンセル
              </button>
              <button type="button" onClick={() => void load()}>
                再読み込み
              </button>
            </div>
          </div>
        )}

        {session && (
          <>
            <p class="selector-picker-instruction">
              {!chosenItem
                ? '記事の1つをクリックしてください。'
                : '（任意）記事のタイトルらしき要素をクリックしてください。'}
            </p>
            {hint && <p class="selector-picker-hint">{hint}</p>}

            <iframe
              ref={iframeRef}
              key={session.view_url}
              src={session.view_url}
              sandbox="allow-scripts"
              referrerPolicy="no-referrer"
              title="ページのプレビュー"
              class="selector-picker-iframe"
            />

            {itemCandidates && (
              <div class="selector-picker-candidates">
                {itemCandidates.length === 0 && (
                  <p class="empty-state">
                    候補を生成できませんでした。別の要素をクリックするか、手動でセレクタを入力してください。
                  </p>
                )}
                <ul>
                  {itemCandidates.map((c, i) => (
                    <li key={c.selector}>
                      <span
                        class="selector-picker-swatch"
                        style={`background:${CANDIDATE_COLORS[i % CANDIDATE_COLORS.length]}`}
                        aria-hidden="true"
                      />
                      <code>{c.selector}</code>
                      <span class="selector-picker-match-count">
                        マッチ数: {c.matchCount}
                      </span>
                      <button type="button" onClick={() => chooseItem(c)}>
                        これを使う
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {chosenItem && (
              <div class="selector-picker-chosen">
                <p>
                  <span
                    class="selector-picker-swatch"
                    style={`background:${CANDIDATE_COLORS[0]}`}
                    aria-hidden="true"
                  />{' '}
                  item_selector: <code>{chosenItem.selector}</code>
                </p>
                <div class="dialog-actions">
                  <button type="button" onClick={restartItemChoice}>
                    選び直す
                  </button>
                  <button type="button" onClick={() => finish(undefined)}>
                    タイトルは指定せず確定
                  </button>
                </div>
              </div>
            )}

            {titleCandidates && (
              <div class="selector-picker-candidates">
                {titleCandidates.length === 0 && (
                  <p class="empty-state">
                    候補を生成できませんでした。別の要素をクリックしてください。
                  </p>
                )}
                <ul>
                  {titleCandidates.map((c, i) => (
                    <li key={c.selector}>
                      <span
                        class="selector-picker-swatch"
                        style={`background:${CANDIDATE_COLORS[i % CANDIDATE_COLORS.length]}`}
                        aria-hidden="true"
                      />
                      <code>{c.selector}</code>
                      <span class="selector-picker-match-count">
                        マッチ数: {c.matchCount}
                      </span>
                      <button type="button" onClick={() => finish(c.selector)}>
                        この設定を使う
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <div class="dialog-actions">
              <button type="button" onClick={onClose}>
                キャンセル
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
