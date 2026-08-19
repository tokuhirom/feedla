import { useState } from 'preact/hooks'
import * as api from '../api/client'
import { applySubscriptionPatch, subscriptions } from '../state/subscriptions'
import { showToast } from '../state/ui'

interface Props {
  feedId: number
}

// The 本文抽出設定 section of FeedDetailOverlay for a normal (kind === 'feed')
// subscription -- unrelated to PagewatchSettings, which is feedless/
// scrape_sources's own settings panel for kind === 'pagewatch' feeds. This
// only ever toggles a single boolean (internal/fulltext), so it's much
// lighter than PagewatchSettings.
export function FulltextSettings({ feedId }: Props) {
  const [toggling, setToggling] = useState(false)

  const sub = subscriptions.value.find((s) => s.feed_id === feedId)
  if (!sub) return null
  const fulltextEnabled = sub.fulltext

  async function handleToggle(): Promise<void> {
    setToggling(true)
    try {
      const updated = fulltextEnabled
        ? await api.disableFulltext(feedId)
        : await api.enableFulltext(feedId)
      applySubscriptionPatch(updated)
    } catch (e) {
      showToast(e instanceof Error ? e.message : String(e))
    } finally {
      setToggling(false)
    }
  }

  return (
    <div class="fulltext-settings">
      <h3>本文抽出設定</h3>
      <p class="fulltext-settings-note">
        フィードが要約しか配信していない記事について、リンク先のページから
        本文を抽出して表示します。有効にすると、新着記事ごとに元記事へ
        追加のフェッチが発生します。
      </p>
      <button
        type="button"
        disabled={toggling}
        onClick={() => void handleToggle()}
      >
        {toggling
          ? '更新中…'
          : fulltextEnabled
            ? '本文抽出を無効にする'
            : '本文抽出を有効にする'}
      </button>
    </div>
  )
}
