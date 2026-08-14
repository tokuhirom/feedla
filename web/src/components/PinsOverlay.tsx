import { useEffect } from 'preact/hooks'
import { loadPins, pins, pinsOpen, removePinById } from '../state/pins'

export function PinsOverlay() {
  useEffect(() => {
    if (pinsOpen.value) void loadPins()
  }, [pinsOpen.value])

  if (!pinsOpen.value) return null

  return (
    <div class="help-overlay" onClick={() => (pinsOpen.value = false)}>
      <div class="help-panel" onClick={(e) => e.stopPropagation()}>
        <h2>pin 一覧</h2>
        {pins.value.length === 0 && <p class="empty-state">pin された記事はありません</p>}
        <ul class="pin-list">
          {pins.value.map((p) => (
            <li key={p.entry_id}>
              <a href={p.url} target="_blank" rel="noopener noreferrer">
                {p.title || p.url}
              </a>
              <button type="button" onClick={() => void removePinById(p.entry_id)}>
                解除
              </button>
            </li>
          ))}
        </ul>
        <button type="button" onClick={() => (pinsOpen.value = false)}>
          閉じる
        </button>
      </div>
    </div>
  )
}
