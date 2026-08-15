import { useEffect } from 'preact/hooks'
import { loadStats, stats, statsOpen } from '../state/stats'

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let value = bytes / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

export function StatsOverlay() {
  useEffect(() => {
    if (statsOpen.value) void loadStats()
  }, [statsOpen.value])

  if (!statsOpen.value) return null

  const s = stats.value

  return (
    <div class="help-overlay" onClick={() => (statsOpen.value = false)}>
      <div class="help-panel" onClick={(e) => e.stopPropagation()}>
        <h2>クロール状況</h2>
        {!s && <p class="empty-state">読み込み中…</p>}
        {s && (
          <dl class="feed-detail-list">
            <dt>購読フィード数</dt>
            <dd>{s.feeds_total}</dd>
            <dt>エラー中のフィード</dt>
            <dd>{s.feeds_erroring}</dd>
            <dt>未読記事数</dt>
            <dd>{s.entries_unread}</dd>
            <dt>次回巡回待ち(due)のフィード</dt>
            <dd>{s.queue_depth}</dd>
            <dt>DB サイズ</dt>
            <dd>{formatBytes(s.db_size_bytes)}</dd>
          </dl>
        )}
        <div class="dialog-actions">
          <button type="button" onClick={() => void loadStats()}>
            更新
          </button>
          <button type="button" onClick={() => (statsOpen.value = false)}>
            閉じる
          </button>
        </div>
      </div>
    </div>
  )
}
