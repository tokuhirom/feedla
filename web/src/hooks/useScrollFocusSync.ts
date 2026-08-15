import { useEffect } from 'preact/hooks'
import { syncFocusToScroll } from '../state/entries'

/** Keeps the focused-entry ring (.entry-item.focused, see EntryItem)
 * following the reading position as the reader scrolls -- mirrors
 * useAutoMarkRead's touch/wheel handling, since touch devices have no j/k
 * to move the ring explicitly.
 */
export function useScrollFocusSync(): void {
  useEffect(() => {
    const root = document.querySelector('.entry-pane')
    if (!root) return

    let rafId: number | null = null
    function onScroll(): void {
      if (rafId !== null) return
      rafId = requestAnimationFrame(() => {
        rafId = null
        syncFocusToScroll()
      })
    }

    root.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      root.removeEventListener('scroll', onScroll)
      if (rafId !== null) cancelAnimationFrame(rafId)
    }
  }, [])
}
