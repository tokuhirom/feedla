import { useEffect } from 'preact/hooks'
import { moveFocus } from '../state/entries'

// Mirrors global.css's mobile breakpoint (see also entries.ts's own copy of
// this) -- swiping is the touch equivalent of j/k, which only makes sense
// once the layout has switched to the single-pane mobile view. Desktop
// already has j/k, and grabbing horizontal drags there too would just be
// surprising (e.g. text selection).
const MOBILE_BREAKPOINT_QUERY = '(max-width: 700px)'

const MIN_SWIPE_PX = 60
// A swipe must stay mostly horizontal to avoid misfiring on what's really
// a vertical scroll -- this caps how much vertical drift is allowed,
// relative to the horizontal distance.
const MAX_OFF_AXIS_RATIO = 0.5
// Swipes starting within this many px of the screen edge are left alone --
// that's the OS/browser's own back-gesture territory (see main.tsx's
// popstate handling for the in-app side of that), and claiming it here
// would fight the native gesture instead of complementing it.
const EDGE_EXCLUSION_PX = 24

/** A swipe starting inside a horizontally-scrollable code block or table
 * (see global.css's .entry-body pre/table overflow-x rules) is the reader
 * scrolling that element's content sideways, not paging -- let it through
 * untouched. */
function startsInHorizontalScrollable(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false
  const el = target.closest('pre, table')
  return !!el && el.scrollWidth > el.clientWidth
}

/** Swipe-to-advance for touch devices: a left swipe moves to the next
 * entry, a right swipe to the previous one, mirroring j/k (see
 * useKeyboardShortcuts) -- so reading through a long entry doesn't require
 * scrolling all the way to its end first just to reach the next one. */
export function useSwipeNavigation(): void {
  useEffect(() => {
    const root = document.querySelector('.entry-pane')
    if (!(root instanceof HTMLElement)) return

    let startX = 0
    let startY = 0
    let tracking = false

    function onTouchStart(e: TouchEvent): void {
      tracking = false
      if (!window.matchMedia(MOBILE_BREAKPOINT_QUERY).matches) return

      const touch = e.touches[0]
      if (!touch) return
      if (
        touch.clientX < EDGE_EXCLUSION_PX ||
        touch.clientX > window.innerWidth - EDGE_EXCLUSION_PX
      )
        return
      if (startsInHorizontalScrollable(e.target)) return

      startX = touch.clientX
      startY = touch.clientY
      tracking = true
    }

    function onTouchEnd(e: TouchEvent): void {
      if (!tracking) return
      tracking = false

      const touch = e.changedTouches[0]
      if (!touch) return
      const dx = touch.clientX - startX
      const dy = touch.clientY - startY
      if (Math.abs(dx) < MIN_SWIPE_PX) return
      if (Math.abs(dy) > Math.abs(dx) * MAX_OFF_AXIS_RATIO) return

      moveFocus(dx < 0 ? 1 : -1)
    }

    function onTouchCancel(): void {
      tracking = false
    }

    root.addEventListener('touchstart', onTouchStart, { passive: true })
    root.addEventListener('touchend', onTouchEnd, { passive: true })
    root.addEventListener('touchcancel', onTouchCancel, { passive: true })
    return () => {
      root.removeEventListener('touchstart', onTouchStart)
      root.removeEventListener('touchend', onTouchEnd)
      root.removeEventListener('touchcancel', onTouchCancel)
    }
  }, [])
}
