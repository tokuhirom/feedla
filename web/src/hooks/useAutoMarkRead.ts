import { useEffect } from 'preact/hooks'
import { markReadOptimistic } from '../state/entries'

/** Marks an entry read once the reader has scrolled past it (its bottom
 * edge has crossed above the entry pane's top edge), so touch users can
 * clear unreads by scrolling alone -- there's no keyboard on a phone to
 * drive the j/k flow moveFocus() implements. Runs alongside j/k rather
 * than replacing it: both funnel into the same idempotent
 * markReadOptimistic, and "scrolled past" already matches what j means on
 * desktop, so enabling this there too is a natural extension, not a
 * behavior change.
 *
 * entryIds is the currently-rendered entry list's ids, used only to
 * re-attach the observer to fresh .entry-item nodes after a re-render
 * (e.g. switching feeds); the ids themselves aren't read.
 */
export function useAutoMarkRead(entryIds: number[]): void {
  useEffect(() => {
    const root = document.querySelector('.entry-pane')
    if (!root) return

    const observer = new IntersectionObserver(
      (observed) => {
        for (const item of observed) {
          if (item.isIntersecting) continue
          // rootBounds is null only if the root itself is detached, which
          // can't happen here since we just queried it from the DOM.
          if (item.boundingClientRect.bottom > item.rootBounds!.top) continue
          const id = Number((item.target as HTMLElement).dataset.entryId)
          if (Number.isFinite(id)) markReadOptimistic(id)
        }
      },
      { root, threshold: 0 },
    )

    for (const el of root.querySelectorAll<HTMLElement>('.entry-item[data-entry-id]')) {
      observer.observe(el)
    }

    return () => observer.disconnect()
  }, [entryIds])
}
